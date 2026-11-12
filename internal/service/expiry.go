package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// ExpiringSecret is a key close to expiry plus its owner group recipients.
type ExpiringSecret struct {
	Key          string
	OrgSlug      string
	ProjectSlug  string
	EnvSlug      string
	ExpiresAt    time.Time
	OwnerGroupID string
	OwnerGroup   string
	Emails       []string
}

// ListExpiringSecrets decrypts key names for items expiring before cutoff.
// Values are never decrypted. Intended for the reminder worker.
func (s *Service) ListExpiringSecrets(ctx context.Context, cutoff time.Time) ([]ExpiringSecret, error) {
	items, err := s.Store.ListExpiringItems(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	type projectKey struct {
		orgID     string
		projectID string
	}
	keys := map[projectKey]*crypto.Key{}
	defer func() {
		for _, key := range keys {
			key.Zero()
		}
	}()
	groupCache := map[string]string{}
	memberCache := map[string][]string{}
	out := make([]ExpiringSecret, 0, len(items))
	for i := range items {
		item := &items[i]
		cacheKey := projectKey{orgID: item.OrgID, projectID: item.ProjectID}
		valueKey := keys[cacheKey]
		if valueKey == nil {
			org, err := s.Store.GetOrgByID(ctx, item.OrgID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				return nil, err
			}
			project, err := s.Store.GetProjectByID(ctx, item.ProjectID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				return nil, err
			}
			derived, indexKey, err := s.ProjectKeys(ctx, org, project)
			if err != nil {
				return nil, err
			}
			indexKey.Zero()
			keys[cacheKey] = &derived
			valueKey = &derived
		}
		plainKey, _, err := valueKey.Decrypt(item.KeyCiphertext,
			crypto.BindAAD(item.OrgID, item.ProjectID, item.EnvID, item.ItemID, "key", 1))
		if err != nil {
			return nil, fmt.Errorf("service: decrypt expiring key: %w", err)
		}
		view := ExpiringSecret{
			Key: string(plainKey), OrgSlug: item.OrgSlug, ProjectSlug: item.ProjectSlug,
			EnvSlug: item.EnvSlug, ExpiresAt: item.ExpiresAt,
		}
		if item.OwnerGroupID != nil {
			view.OwnerGroupID = *item.OwnerGroupID
			if name, ok := groupCache[*item.OwnerGroupID]; ok {
				view.OwnerGroup = name
			} else if group, err := s.Store.GetGroup(ctx, item.OrgID, *item.OwnerGroupID); err == nil {
				view.OwnerGroup = group.Name
				groupCache[*item.OwnerGroupID] = group.Name
			}
			emails, ok := memberCache[*item.OwnerGroupID]
			if !ok {
				members, err := s.Store.ListGroupMembers(ctx, *item.OwnerGroupID)
				if err == nil {
					for _, member := range members {
						emails = append(emails, member.Email)
					}
				}
				memberCache[*item.OwnerGroupID] = emails
			}
			view.Emails = emails
		}
		out = append(out, view)
	}
	return out, nil
}
