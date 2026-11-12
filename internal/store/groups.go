package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Group is an org-level set of users that can receive role bindings.
type Group struct {
	ID         string
	OrgID      string
	Name       string
	ExternalID string
	CreatedAt  time.Time
}

// GroupMember is a user in a group.
type GroupMember struct {
	UserID string
	Email  string
	Name   string
}

// CreateGroup inserts a group.
func (s *Store) CreateGroup(ctx context.Context, group *Group) error {
	if group.ID == "" {
		group.ID = uuid.NewString()
	}
	if group.CreatedAt.IsZero() {
		group.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO groups (id, org_id, name, external_id, created_at)
		VALUES (?,?,?,?,?)`, group.ID, group.OrgID, group.Name, nullString(group.ExternalID), formatTime(group.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create group: %w", err)
	}
	return nil
}

// ListGroups returns an org's groups.
func (s *Store) ListGroups(ctx context.Context, orgID string) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, org_id, name, COALESCE(external_id, ''), created_at
		FROM groups WHERE org_id = ? ORDER BY name`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list groups: %w", err)
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var group Group
		var createdAt string
		if err := rows.Scan(&group.ID, &group.OrgID, &group.Name, &group.ExternalID, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan group: %w", err)
		}
		if group.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, group)
	}
	return out, rows.Err()
}

// GetGroup loads a group in an org.
func (s *Store) GetGroup(ctx context.Context, orgID, id string) (*Group, error) {
	var group Group
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, org_id, name, COALESCE(external_id, ''), created_at
		FROM groups WHERE id = ? AND org_id = ?`, id, orgID).
		Scan(&group.ID, &group.OrgID, &group.Name, &group.ExternalID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get group: %w", err)
	}
	if group.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &group, nil
}

// RenameGroup updates a group name.
func (s *Store) RenameGroup(ctx context.Context, orgID, id, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE groups SET name = ? WHERE id = ? AND org_id = ?`, name, id, orgID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: rename group: %w", err)
	}
	return expectOne(res, "rename group")
}

// DeleteGroup removes a group and its bindings.
func (s *Store) DeleteGroup(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = ? AND org_id = ?`, id, orgID)
	if err != nil {
		return fmt.Errorf("store: delete group: %w", err)
	}
	return expectOne(res, "delete group")
}

// AddGroupMember adds a user to a group.
func (s *Store) AddGroupMember(ctx context.Context, groupID, userID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id) VALUES (?,?)`, groupID, userID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: add group member: %w", err)
	}
	return nil
}

// RemoveGroupMember removes a user from a group.
func (s *Store) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID)
	if err != nil {
		return fmt.Errorf("store: remove group member: %w", err)
	}
	return expectOne(res, "remove group member")
}

// ListGroupMembers returns a group's users.
func (s *Store) ListGroupMembers(ctx context.Context, groupID string) ([]GroupMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, u.name
		FROM group_members gm JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = ? ORDER BY u.name, u.email`, groupID)
	if err != nil {
		return nil, fmt.Errorf("store: list group members: %w", err)
	}
	defer rows.Close()
	var out []GroupMember
	for rows.Next() {
		var member GroupMember
		if err := rows.Scan(&member.UserID, &member.Email, &member.Name); err != nil {
			return nil, fmt.Errorf("store: scan group member: %w", err)
		}
		out = append(out, member)
	}
	return out, rows.Err()
}

// ListUserGroupIDs returns the group IDs a user belongs to.
func (s *Store) ListUserGroupIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id FROM group_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list user groups: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan user group: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
