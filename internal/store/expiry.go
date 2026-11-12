package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ExpiringItem is an encrypted item close to its expiry date.
type ExpiringItem struct {
	ItemID        string
	OrgID         string
	OrgSlug       string
	ProjectID     string
	ProjectSlug   string
	KeyVersion    int
	EnvID         string
	EnvSlug       string
	KeyCiphertext []byte
	OwnerGroupID  *string
	ExpiresAt     time.Time
}

// ListExpiringItems returns non-overlay items expiring before the cutoff.
func (s *Store) ListExpiringItems(ctx context.Context, cutoff time.Time) ([]ExpiringItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, o.id, o.slug, p.id, p.slug, p.key_version,
			e.id, e.slug, i.key_ciphertext, i.owner_group_id, i.expires_at
		FROM items i
		JOIN environments e ON e.id = i.environment_id
		JOIN projects p ON p.id = e.project_id
		JOIN orgs o ON o.id = p.org_id
		WHERE i.overlay_id IS NULL AND i.deleted_at IS NULL
		  AND i.expires_at IS NOT NULL AND i.expires_at <= ?
		ORDER BY i.expires_at ASC`, formatTime(cutoff))
	if err != nil {
		return nil, fmt.Errorf("store: list expiring items: %w", err)
	}
	defer rows.Close()
	var out []ExpiringItem
	for rows.Next() {
		var item ExpiringItem
		var ownerGroupID sql.NullString
		var expiresAt string
		if err := rows.Scan(&item.ItemID, &item.OrgID, &item.OrgSlug, &item.ProjectID, &item.ProjectSlug,
			&item.KeyVersion, &item.EnvID, &item.EnvSlug, &item.KeyCiphertext, &ownerGroupID, &expiresAt); err != nil {
			return nil, fmt.Errorf("store: scan expiring item: %w", err)
		}
		if ownerGroupID.Valid {
			item.OwnerGroupID = &ownerGroupID.String
		}
		parsed, err := parseTime(expiresAt)
		if err != nil {
			return nil, err
		}
		item.ExpiresAt = parsed
		out = append(out, item)
	}
	return out, rows.Err()
}
