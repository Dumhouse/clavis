package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PromotionRequest is a pending promotion to a protected environment.
type PromotionRequest struct {
	ID          string
	OrgID       string
	ProjectID   string
	SourceEnv   string
	TargetEnv   string
	Keys        string
	Status      string
	RequestedBy string
	RequestedAt time.Time
	DecidedBy   string
	DecidedAt   *time.Time
}

const promotionColumns = `id, org_id, project_id, source_env, target_env, keys, status,
	requested_by, requested_at, decided_by, decided_at`

func scanPromotionRequest(row rowScanner) (*PromotionRequest, error) {
	var item PromotionRequest
	var requestedAt string
	var decidedBy, decidedAt sql.NullString
	if err := row.Scan(&item.ID, &item.OrgID, &item.ProjectID, &item.SourceEnv, &item.TargetEnv,
		&item.Keys, &item.Status, &item.RequestedBy, &requestedAt, &decidedBy, &decidedAt); err != nil {
		return nil, err
	}
	item.DecidedBy = decidedBy.String
	var err error
	if item.RequestedAt, err = parseTime(requestedAt); err != nil {
		return nil, err
	}
	if decidedAt.Valid {
		parsed, err := parseTime(decidedAt.String)
		if err != nil {
			return nil, err
		}
		item.DecidedAt = &parsed
	}
	return &item, nil
}

// CreatePromotionRequest records a pending protected-environment promotion.
func (s *Store) CreatePromotionRequest(ctx context.Context, request *PromotionRequest) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO promotion_requests
		(id, org_id, project_id, source_env, target_env, keys, status, requested_by, requested_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		request.ID, request.OrgID, request.ProjectID, request.SourceEnv, request.TargetEnv,
		request.Keys, request.Status, request.RequestedBy, formatTime(request.RequestedAt))
	if err != nil {
		return fmt.Errorf("store: create promotion request: %w", err)
	}
	return nil
}

// ListPromotionRequests returns requests for a project, newest first.
func (s *Store) ListPromotionRequests(ctx context.Context, projectID, status string) ([]PromotionRequest, error) {
	query := `SELECT ` + promotionColumns + ` FROM promotion_requests WHERE project_id = ?`
	args := []any{projectID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY requested_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list promotion requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PromotionRequest
	for rows.Next() {
		item, err := scanPromotionRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan promotion request: %w", err)
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// GetPromotionRequest loads one request.
func (s *Store) GetPromotionRequest(ctx context.Context, projectID, id string) (*PromotionRequest, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+promotionColumns+` FROM promotion_requests
		WHERE project_id = ? AND id = ?`, projectID, id)
	item, err := scanPromotionRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get promotion request: %w", err)
	}
	return item, nil
}

// DecidePromotionRequest updates the status and decision metadata.
func (s *Store) DecidePromotionRequest(ctx context.Context, projectID, id, status, decidedBy string, decidedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE promotion_requests
		SET status = ?, decided_by = ?, decided_at = ?
		WHERE project_id = ? AND id = ? AND status = 'pending'`,
		status, decidedBy, formatTime(decidedAt), projectID, id)
	if err != nil {
		return fmt.Errorf("store: decide promotion request: %w", err)
	}
	return expectOne(res, "decide promotion request")
}
