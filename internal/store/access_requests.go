package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AccessRequest is a just-in-time access request.
type AccessRequest struct {
	ID                string
	OrgID             string
	ProjectID         string
	RequesterID       string
	Environment       string
	Role              string
	Reason            string
	Status            string
	DurationSeconds   int
	RequiredApprovals int
	Approvals         string
	ExpiresAt         *time.Time
	DecidedAt         *time.Time
	AppliedBindingID  string
	CreatedAt         time.Time
}

const accessRequestColumns = `id, org_id, project_id, requester_id, environment, role, reason,
	status, duration_seconds, required_approvals, approvals, expires_at, decided_at,
	applied_binding_id, created_at`

func scanAccessRequest(row rowScanner) (*AccessRequest, error) {
	var req AccessRequest
	var expiresAt, decidedAt sql.NullString
	var createdAt string
	if err := row.Scan(&req.ID, &req.OrgID, &req.ProjectID, &req.RequesterID, &req.Environment,
		&req.Role, &req.Reason, &req.Status, &req.DurationSeconds, &req.RequiredApprovals,
		&req.Approvals, &expiresAt, &decidedAt, &req.AppliedBindingID, &createdAt); err != nil {
		return nil, err
	}
	var err error
	if expiresAt.Valid {
		parsed, err := parseTime(expiresAt.String)
		if err != nil {
			return nil, err
		}
		req.ExpiresAt = &parsed
	}
	if decidedAt.Valid {
		parsed, err := parseTime(decidedAt.String)
		if err != nil {
			return nil, err
		}
		req.DecidedAt = &parsed
	}
	if req.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &req, nil
}

func (s *Store) CreateAccessRequest(ctx context.Context, req *AccessRequest) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO access_requests
		(id, org_id, project_id, requester_id, target, environment, role, reason, status,
		 duration_seconds, required_approvals, approvals, expires_at, decided_at,
		 applied_binding_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.ID, req.OrgID, req.ProjectID, req.RequesterID, req.Environment, req.Environment,
		req.Role, req.Reason, req.Status, req.DurationSeconds, req.RequiredApprovals,
		req.Approvals, nullableTime(req.ExpiresAt), nullableTime(req.DecidedAt),
		req.AppliedBindingID, formatTime(req.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create access request: %w", err)
	}
	return nil
}

func (s *Store) GetAccessRequest(ctx context.Context, projectID, id string) (*AccessRequest, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+accessRequestColumns+` FROM access_requests
		WHERE project_id = ? AND id = ?`, projectID, id)
	req, err := scanAccessRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get access request: %w", err)
	}
	return req, nil
}

func (s *Store) ListAccessRequests(ctx context.Context, projectID, status, requesterID string) ([]AccessRequest, error) {
	query := `SELECT ` + accessRequestColumns + ` FROM access_requests WHERE project_id = ?`
	args := []any{projectID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if requesterID != "" {
		query += ` AND requester_id = ?`
		args = append(args, requesterID)
	}
	query += ` ORDER BY created_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list access requests: %w", err)
	}
	defer rows.Close()
	var out []AccessRequest
	for rows.Next() {
		req, err := scanAccessRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *req)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAccessRequest(ctx context.Context, req *AccessRequest) error {
	res, err := s.db.ExecContext(ctx, `UPDATE access_requests SET status = ?, approvals = ?,
		decided_at = ?, applied_binding_id = ?, expires_at = ? WHERE id = ?`,
		req.Status, req.Approvals, nullableTime(req.DecidedAt), req.AppliedBindingID,
		nullableTime(req.ExpiresAt), req.ID)
	if err != nil {
		return err
	}
	return expectOne(res, "update access request")
}
