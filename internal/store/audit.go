package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dumhouse/clavis/internal/audit"
)

const auditColumns = `id, org_id, seq, prev_hash, hash, actor_id, actor_kind, actor_name,
	action, resource_type, resource_id, resource_name, project_id, env_id,
	details, ip, user_agent, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAuditEvent(row rowScanner) (*audit.Event, error) {
	var (
		ev                 audit.Event
		createdAt, details string
	)
	err := row.Scan(
		&ev.ID, &ev.OrgID, &ev.Seq, &ev.PrevHash, &ev.Hash,
		&ev.Actor.ID, &ev.Actor.Kind, &ev.Actor.Name,
		&ev.Action, &ev.Resource.Type, &ev.Resource.ID, &ev.Resource.Name,
		&ev.Resource.ProjectID, &ev.Resource.EnvID,
		&details, &ev.IP, &ev.UserAgent, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan audit event: %w", err)
	}
	if details != "" {
		if err := json.Unmarshal([]byte(details), &ev.Details); err != nil {
			return nil, fmt.Errorf("store: decode audit details: %w", err)
		}
	}
	ev.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// AppendAuditEvent atomically appends an event to an org's hash chain. It
// retries when two writers race for the same sequence number.
func (s *Store) AppendAuditEvent(ctx context.Context, ev *audit.Event) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = s.appendAuditEventOnce(ctx, ev)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
	}
	return fmt.Errorf("store: append audit event: %w", err)
}

func (s *Store) appendAuditEventOnce(ctx context.Context, ev *audit.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin audit tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var prev *audit.Event
	prev, err = scanAuditEvent(tx.QueryRowContext(ctx,
		`SELECT `+auditColumns+` FROM audit_events WHERE org_id = ? ORDER BY seq DESC LIMIT 1`, ev.OrgID))
	if errors.Is(err, ErrNotFound) {
		prev = nil
	} else if err != nil {
		return err
	}

	if err := audit.Seal(prev, ev); err != nil {
		return err
	}
	details, err := json.Marshal(ev.Details)
	if err != nil {
		return fmt.Errorf("store: encode audit details: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events (
		id, org_id, seq, prev_hash, hash, actor_id, actor_kind, actor_name,
		action, resource_type, resource_id, resource_name, project_id, env_id,
		details, ip, user_agent, created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.ID, ev.OrgID, ev.Seq, ev.PrevHash, ev.Hash,
		ev.Actor.ID, ev.Actor.Kind, ev.Actor.Name,
		ev.Action, ev.Resource.Type, ev.Resource.ID, ev.Resource.Name,
		ev.Resource.ProjectID, ev.Resource.EnvID,
		string(details), ev.IP, ev.UserAgent, formatTime(ev.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: insert audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit audit event: %w", err)
	}
	return nil
}

// LastAuditEvent returns the newest event for an org, or nil if the chain is
// empty.
func (s *Store) LastAuditEvent(ctx context.Context, orgID string) (*audit.Event, error) {
	ev, err := scanAuditEvent(s.db.QueryRowContext(ctx,
		`SELECT `+auditColumns+` FROM audit_events WHERE org_id = ? ORDER BY seq DESC LIMIT 1`, orgID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return ev, err
}

// ListAuditEvents returns events after afterSeq in ascending order.
func (s *Store) ListAuditEvents(ctx context.Context, orgID string, afterSeq int64, limit int) ([]audit.Event, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+auditColumns+` FROM audit_events WHERE org_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`,
		orgID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []audit.Event
	for rows.Next() {
		ev, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate audit events: %w", err)
	}
	return events, nil
}

// VerifyAuditChain recomputes an org's chain.
func (s *Store) VerifyAuditChain(ctx context.Context, orgID string) error {
	events, err := s.ListAuditEvents(ctx, orgID, 0, 0)
	if err != nil {
		return err
	}
	if err := audit.Verify(events); err != nil {
		return fmt.Errorf("store: audit chain for org %s: %w", orgID, err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
