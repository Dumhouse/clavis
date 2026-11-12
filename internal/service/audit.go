package service

import (
	"context"
	"errors"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// ListAuditEvents returns an org's audit events after afterSeq and whether the
// full chain verifies. Only AuditRead holders may call it.
func (s *Service) ListAuditEvents(ctx context.Context, actor authz.Actor, orgSlug string, afterSeq int64, limit int) ([]audit.Event, bool, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	if err := s.Authorize(actor, authz.AuditRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, false, err
	}
	events, err := s.Store.ListAuditEvents(ctx, org.ID, afterSeq, limit)
	if err != nil {
		return nil, false, err
	}
	verified := s.Store.VerifyAuditChain(ctx, org.ID) == nil
	return events, verified, nil
}

// RecordAuthEvent writes an authentication event to each org the user belongs
// to. Authentication is not scoped to one org, so it is recorded everywhere the
// person can see it.
func (s *Service) RecordAuthEvent(ctx context.Context, user *store.User, action, channel string, meta AuditMeta) {
	memberships, err := s.Store.ListOrgsForUser(ctx, user.ID)
	if err != nil {
		s.Logger.Error("record auth event failed", "user_id", user.ID, "action", action, "error", err)
		return
	}
	actor := audit.Actor{ID: user.ID, Kind: string(authz.SubjectUser), Name: user.Name}
	for _, membership := range memberships {
		if err := s.record(ctx, actor, membership.Org.ID, action,
			audit.Resource{Type: "user", ID: user.ID}, audit.Details{Channel: channel}, meta); err != nil {
			s.Logger.Error("append auth event failed", "org_id", membership.Org.ID, "action", action, "error", err)
		}
	}
}
