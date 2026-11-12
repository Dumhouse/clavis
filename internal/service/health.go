package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/dumhouse/clavis/internal/authz"
)

// HealthFinding is one actionable health issue.
type HealthFinding struct {
	Category string
	Level    string // warning | critical
	Message  string
	Project  string
	Env      string
	Key      string
	Subject  string
}

// HealthReport is an organization/project health snapshot.
type HealthReport struct {
	Score    int
	Findings []HealthFinding
	Checked  int
}

// Health checks secrets, rotation coverage, expiries, MFA, and token lifetimes.
func (s *Service) Health(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string) (*HealthReport, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		return nil, ErrNotFound
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	projects, err := s.Store.ListProjects(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	report := &HealthReport{Score: 100}
	settings := ParseOrgSettings(org.Settings)
	now := s.Now().UTC()
	penalty := 0
	for i := range projects {
		project := &projects[i]
		if projectSlug != "" && project.Slug != projectSlug {
			continue
		}
		envs, err := s.Store.ListEnvironments(ctx, project.ID)
		if err != nil {
			continue
		}
		unmanaged := 0
		fingerprints := map[string][]string{}
		for e := range envs {
			env := &envs[e]
			secrets, _, err := s.ListSecrets(ctx, actor, orgSlug, project.Slug, env.Slug, settings.DuplicateDetection, "health", AuditMeta{})
			if err != nil {
				continue
			}
			report.Checked += len(secrets)
			for _, secret := range secrets {
				age := now.Sub(secret.UpdatedAt)
				if age > 90*24*time.Hour {
					report.Findings = append(report.Findings, HealthFinding{
						Category: "stale", Level: "warning", Project: project.Slug, Env: env.Slug,
						Key: secret.Key, Message: fmt.Sprintf("%s is %d days old", secret.Key, int(age.Hours()/24)),
					})
					penalty += 2
				}
				lastReveal, revealErr := s.Store.LastRevealAt(ctx, secret.ItemID)
				if revealErr == nil && (lastReveal == nil || now.Sub(*lastReveal) > 90*24*time.Hour) && age > 90*24*time.Hour {
					report.Findings = append(report.Findings, HealthFinding{
						Category: "unused", Level: "warning", Project: project.Slug, Env: env.Slug,
						Key: secret.Key, Message: "not revealed in 90 days",
					})
					penalty += 2
				}
				if settings.DuplicateDetection && secret.Value != "" {
					sum := sha256.Sum256([]byte(secret.Value))
					fingerprint := hex.EncodeToString(sum[:])
					fingerprints[fingerprint] = append(fingerprints[fingerprint], env.Slug+"/"+secret.Key)
				}
				if secret.Version == 1 && age > 30*24*time.Hour {
					report.Findings = append(report.Findings, HealthFinding{
						Category: "never-rotated", Level: "warning", Project: project.Slug,
						Env: env.Slug, Key: secret.Key, Message: "never rotated",
					})
					penalty += 1
				}
				if secret.ExpiresAt != nil && secret.ExpiresAt.Before(now.Add(30*24*time.Hour)) {
					report.Findings = append(report.Findings, HealthFinding{
						Category: "expiring", Level: "warning", Project: project.Slug, Env: env.Slug,
						Key: secret.Key, Message: "expires within 30 days",
					})
					penalty += 2
				}
			}
			status, err := s.RotationStatusForEnv(ctx, actor, orgSlug, project.Slug, env.Slug, 90)
			if err == nil && status.Unmanaged > 0 {
				unmanaged += status.Unmanaged
			}
		}
		if settings.DuplicateDetection {
			for _, keys := range fingerprints {
				if len(keys) > 1 {
					report.Findings = append(report.Findings, HealthFinding{
						Category: "duplicate-value", Level: "warning", Project: project.Slug,
						Message: "duplicate value: " + joinStrings(keys),
					})
					penalty += 2
				}
			}
		}
		if unmanaged > 0 {
			report.Findings = append(report.Findings, HealthFinding{
				Category: "no-rotation-policy", Level: "warning", Project: project.Slug,
				Message: fmt.Sprintf("%d secrets have no rotation policy", unmanaged),
			})
			penalty += unmanaged
		}
	}
	members, err := s.Store.ListMembers(ctx, org.ID)
	if err == nil {
		for _, member := range members {
			user, err := s.Store.GetUser(ctx, member.UserID)
			if err == nil && !user.MFAEnabled {
				report.Findings = append(report.Findings, HealthFinding{
					Category: "no-mfa", Level: "critical", Subject: member.Email,
					Message: "member has no MFA",
				})
				penalty += 5
			}
		}
	}
	identities, err := s.Store.ListIdentities(ctx, org.ID)
	if err == nil {
		for i := range identities {
			tokens, err := s.Store.ListTokensForIdentity(ctx, identities[i].ID)
			if err != nil {
				continue
			}
			for _, token := range tokens {
				if token.ExpiresAt == nil {
					report.Findings = append(report.Findings, HealthFinding{
						Category: "non-expiring-token", Level: "warning", Subject: identities[i].Name,
						Message: "identity token never expires",
					})
					penalty += 3
				}
			}
		}
	}
	report.Score = 100 - penalty
	if report.Score < 0 {
		report.Score = 0
	}
	return report, nil
}

func joinStrings(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ", "
		}
		out += value
	}
	return out
}
