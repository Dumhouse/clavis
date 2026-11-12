package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// OffboardingTask is one revealed secret to rotate or mark safe.
type OffboardingTask struct {
	ID        string
	OrgID     string
	UserID    string
	ProjectID string
	EnvID     string
	ItemID    string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// OffboardingItem identifies a secret revealed by a departing user.
type OffboardingItem struct {
	ItemID    string
	ProjectID string
	EnvID     string
}

// DeleteUserPersonalTokens revokes a user's personal tokens for an org.
func (s *Store) DeleteUserPersonalTokens(ctx context.Context, userID, orgID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM access_tokens WHERE user_id = ? AND org_id = ? AND identity_id IS NULL`,
		userID, orgID)
	if err != nil {
		return fmt.Errorf("store: delete personal tokens: %w", err)
	}
	return nil
}

// ListUserRevealedItems returns distinct secrets the user revealed in an org.
func (s *Store) ListUserRevealedItems(ctx context.Context, orgID, userID string) ([]OffboardingItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT i.id, i.environment_id, e.project_id
		FROM reveal_events re
		JOIN items i ON i.id = re.item_id
		JOIN environments e ON e.id = i.environment_id
		JOIN projects p ON p.id = e.project_id
		WHERE re.user_id = ? AND p.org_id = ?
		ORDER BY i.id`, userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list user reveals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []OffboardingItem
	for rows.Next() {
		var item OffboardingItem
		if err := rows.Scan(&item.ItemID, &item.EnvID, &item.ProjectID); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// CreateOffboardingTask inserts a task idempotently.
func (s *Store) CreateOffboardingTask(ctx context.Context, task *OffboardingTask) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO offboarding_tasks
		(id, org_id, user_id, project_id, env_id, item_id, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id, item_id) DO NOTHING`,
		task.ID, task.OrgID, task.UserID, task.ProjectID, task.EnvID, task.ItemID,
		task.Status, formatTime(task.CreatedAt), formatTime(task.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: create offboarding task: %w", err)
	}
	return nil
}

// ListOffboardingTasks returns tasks for a user.
func (s *Store) ListOffboardingTasks(ctx context.Context, orgID, userID string) ([]OffboardingTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, org_id, user_id, project_id, env_id, item_id,
		status, created_at, updated_at FROM offboarding_tasks
		WHERE org_id = ? AND user_id = ? ORDER BY created_at, id`, orgID, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list offboarding tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []OffboardingTask
	for rows.Next() {
		var task OffboardingTask
		var createdAt, updatedAt string
		if err := rows.Scan(&task.ID, &task.OrgID, &task.UserID, &task.ProjectID, &task.EnvID,
			&task.ItemID, &task.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var err error
		if task.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if task.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

// UpdateOffboardingTaskStatus marks a task rotated or safe.
func (s *Store) UpdateOffboardingTaskStatus(ctx context.Context, orgID, userID, id, status string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE offboarding_tasks SET status = ?, updated_at = ?
		WHERE org_id = ? AND user_id = ? AND id = ?`, status, formatTime(at), orgID, userID, id)
	if err != nil {
		return err
	}
	return expectOne(res, "update offboarding task")
}

// CountUserAuditActions counts audit events by action for a user.
func (s *Store) CountUserAuditActions(ctx context.Context, orgID, userID, action string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events
		WHERE org_id = ? AND actor_id = ? AND action = ?`, orgID, userID, action).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}

// CountUserExportReveals counts reveal events with the export channel.
func (s *Store) CountUserExportReveals(ctx context.Context, orgID, userID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events
		WHERE org_id = ? AND actor_id = ? AND action = 'secret.list_reveal'
		  AND details LIKE '%"channel":"export"%'`, orgID, userID).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}
