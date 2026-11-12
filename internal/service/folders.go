package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// ListFolders returns folders in an environment.
func (s *Service) ListFolders(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) ([]store.Folder, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SecretsReadNames, secretScope(org, project, env)); err != nil {
		return nil, err
	}
	return s.Store.ListFolders(ctx, env.ID)
}

// CreateFolder adds a folder path.
func (s *Service) CreateFolder(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, path string, meta AuditMeta) (*store.Folder, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.EnvironmentsManage, secretScope(org, project, env)); err != nil {
		return nil, err
	}
	clean := authz.NormalizePath(strings.TrimSpace(path))
	if clean == "/" || clean == "." {
		return nil, fmt.Errorf("%w: folder path must be below the root", ErrValidation)
	}
	folder := &store.Folder{ID: uuid.NewString(), EnvironmentID: env.ID, Path: clean}
	if err := s.Store.CreateFolder(ctx, folder); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	s.publish(org.ID, "secrets.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "folder.create",
		audit.Resource{Type: "folder", ID: folder.ID, Name: folder.Path, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Path: folder.Path}, meta); err != nil {
		s.Logger.Error("audit folder.create failed", "folder_id", folder.ID, "error", err)
	}
	return folder, nil
}

// DeleteFolder removes a folder path.
func (s *Service) DeleteFolder(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, folderID string, meta AuditMeta) error {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if err := s.Authorize(actor, authz.EnvironmentsManage, secretScope(org, project, env)); err != nil {
		return err
	}
	if err := s.Store.DeleteFolder(ctx, env.ID, folderID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.publish(org.ID, "secrets.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "folder.delete",
		audit.Resource{Type: "folder", ID: folderID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, meta); err != nil {
		s.Logger.Error("audit folder.delete failed", "folder_id", folderID, "error", err)
	}
	return nil
}
