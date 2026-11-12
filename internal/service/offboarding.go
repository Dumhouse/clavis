package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// OffboardingTaskView is a revealed secret to rotate or mark safe.
type OffboardingTaskView struct {
	ID          string
	ProjectSlug string
	EnvSlug     string
	Key         string
	Status      string
	UpdatedAt   time.Time
}

// OffboardingReport tracks a departing member's revealed secrets.
type OffboardingReport struct {
	UserID   string
	Email    string
	Name     string
	Tasks    []OffboardingTaskView
	Done     int
	Total    int
	Exported int
}

// StartOffboarding creates tasks for every secret the user revealed.
func (s *Service) StartOffboarding(ctx context.Context, actor authz.Actor, orgSlug, userID string, meta AuditMeta) (*OffboardingReport, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.GetMembership(ctx, org.ID, userID); err != nil {
		return nil, ErrNotFound
	}
	if err := s.createOffboardingTasks(ctx, org.ID, userID); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "member.offboarding.start",
		audit.Resource{Type: "member", ID: userID}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit member.offboarding.start failed", "user_id", userID, "error", err)
	}
	return s.OffboardingReport(ctx, actor, orgSlug, userID)
}

func (s *Service) createOffboardingTasks(ctx context.Context, orgID, userID string) error {
	items, err := s.Store.ListUserRevealedItems(ctx, orgID, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		now := s.Now().UTC()
		task := &store.OffboardingTask{
			ID: uuid.NewString(), OrgID: orgID, UserID: userID, ProjectID: item.ProjectID,
			EnvID: item.EnvID, ItemID: item.ItemID, Status: "pending",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.Store.CreateOffboardingTask(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

// OffboardingReport lists tasks, resolving key names without revealing values.
func (s *Service) OffboardingReport(ctx context.Context, actor authz.Actor, orgSlug, userID string) (*OffboardingReport, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	membership, err := s.Store.GetMembership(ctx, org.ID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	user, _ := s.Store.GetUser(ctx, userID)
	tasks, err := s.Store.ListOffboardingTasks(ctx, org.ID, userID)
	if err != nil {
		return nil, err
	}
	report := &OffboardingReport{UserID: userID}
	if user != nil {
		report.Email = user.Email
		report.Name = user.Name
	}
	if membership == nil {
		return report, nil
	}
	// Decrypt key names grouped by project to derive each project key once.
	type projectCache struct {
		valueKey *crypto.Key
	}
	caches := map[string]*projectCache{}
	defer func() {
		for _, cache := range caches {
			if cache.valueKey != nil {
				cache.valueKey.Zero()
			}
		}
	}()
	for i := range tasks {
		task := &tasks[i]
		cache := caches[task.ProjectID]
		if cache == nil {
			project, err := s.Store.GetProjectByID(ctx, task.ProjectID)
			if err != nil {
				continue
			}
			orgRow, err := s.Store.GetOrgByID(ctx, org.ID)
			if err != nil {
				continue
			}
			valueKey, _, err := s.ProjectKeys(ctx, orgRow, project)
			if err != nil {
				continue
			}
			cache = &projectCache{valueKey: &valueKey}
			caches[task.ProjectID] = cache
		}
		item, err := s.Store.GetItemByID(ctx, task.ItemID)
		if err != nil {
			continue
		}
		env, err := s.Store.GetEnvironmentByID(ctx, task.EnvID)
		if err != nil {
			continue
		}
		project, err := s.Store.GetProjectByID(ctx, task.ProjectID)
		if err != nil {
			continue
		}
		plain, _, err := cache.valueKey.Decrypt(item.KeyCiphertext,
			crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "key", 1))
		if err != nil {
			continue
		}
		report.Tasks = append(report.Tasks, OffboardingTaskView{
			ID: task.ID, ProjectSlug: project.Slug, EnvSlug: env.Slug,
			Key: string(plain), Status: task.Status, UpdatedAt: task.UpdatedAt,
		})
		if task.Status != "pending" {
			report.Done++
		}
	}
	report.Total = len(report.Tasks)
	report.Exported, _ = s.Store.CountUserExportReveals(ctx, org.ID, userID)
	return report, nil
}

// MarkOffboardingTask marks a revealed secret rotated or safe.
func (s *Service) MarkOffboardingTask(ctx context.Context, actor authz.Actor, orgSlug, userID, taskID, status string, meta AuditMeta) (*OffboardingReport, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if status != "rotated" && status != "safe" {
		return nil, fmt.Errorf("%w: status must be rotated or safe", ErrValidation)
	}
	if err := s.Store.UpdateOffboardingTaskStatus(ctx, org.ID, userID, taskID, status, s.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "member.offboarding."+status,
		audit.Resource{Type: "member", ID: userID}, audit.Details{Reason: taskID}, meta); err != nil {
		s.Logger.Error("audit offboarding task failed", "task_id", taskID, "error", err)
	}
	return s.OffboardingReport(ctx, actor, orgSlug, userID)
}
