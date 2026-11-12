package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
)

// CreateAuditCheckpoint stores a signed audit checkpoint.
func (s *Store) CreateAuditCheckpoint(ctx context.Context, checkpoint *audit.Checkpoint) error {
	if checkpoint.ID == "" {
		checkpoint.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_checkpoints
		(id, org_id, seq, event_hash, signature, public_key, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		checkpoint.ID, checkpoint.OrgID, checkpoint.Seq, checkpoint.EventHash,
		checkpoint.Signature, checkpoint.PublicKey, formatTime(checkpoint.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create audit checkpoint: %w", err)
	}
	return nil
}

// ListAuditCheckpoints returns an org's checkpoints in sequence order.
func (s *Store) ListAuditCheckpoints(ctx context.Context, orgID string) ([]audit.Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, org_id, seq, event_hash, signature, public_key, created_at
		FROM audit_checkpoints WHERE org_id = ? ORDER BY seq, created_at`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list audit checkpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []audit.Checkpoint
	for rows.Next() {
		var checkpoint audit.Checkpoint
		var createdAt string
		if err := rows.Scan(&checkpoint.ID, &checkpoint.OrgID, &checkpoint.Seq, &checkpoint.EventHash,
			&checkpoint.Signature, &checkpoint.PublicKey, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan audit checkpoint: %w", err)
		}
		if checkpoint.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, checkpoint)
	}
	return out, rows.Err()
}

// LastAuditCheckpoint returns the most recent checkpoint, or nil.
func (s *Store) LastAuditCheckpoint(ctx context.Context, orgID string) (*audit.Checkpoint, error) {
	var checkpoint audit.Checkpoint
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, org_id, seq, event_hash, signature, public_key, created_at
		FROM audit_checkpoints WHERE org_id = ? ORDER BY seq DESC, created_at DESC LIMIT 1`, orgID).Scan(
		&checkpoint.ID, &checkpoint.OrgID, &checkpoint.Seq, &checkpoint.EventHash,
		&checkpoint.Signature, &checkpoint.PublicKey, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get last audit checkpoint: %w", err)
	}
	if checkpoint.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &checkpoint, nil
}
