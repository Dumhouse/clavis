package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Invite grants one role in an org. Only the hash of the link token is stored.
type Invite struct {
	ID         string
	OrgID      string
	Email      string
	RoleID     string
	RoleName   string
	TokenHash  []byte
	CreatedBy  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	AcceptedBy string
}

// Member is a user's membership with display fields.
type Member struct {
	UserID    string
	Email     string
	Name      string
	RoleID    string
	RoleName  string
	Status    string
	CreatedAt time.Time
	RemovedAt *time.Time
}

// CreateInvite inserts an invite.
func (s *Store) CreateInvite(ctx context.Context, invite *Invite) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO invites
		(id, org_id, email, role_id, token_hash, created_by, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		invite.ID, invite.OrgID, invite.Email, invite.RoleID, invite.TokenHash,
		invite.CreatedBy, formatTime(invite.CreatedAt), formatTime(invite.ExpiresAt))
	if err != nil {
		return fmt.Errorf("store: create invite: %w", err)
	}
	return nil
}

const inviteColumns = `i.id, i.org_id, i.email, i.role_id, COALESCE(r.name, ''), i.token_hash,
	i.created_by, i.created_at, i.expires_at, i.accepted_at, COALESCE(i.accepted_by, '')`

func scanInvite(row rowScanner) (*Invite, error) {
	var (
		invite               Invite
		createdAt, expiresAt string
		acceptedAt           sql.NullString
	)
	err := row.Scan(&invite.ID, &invite.OrgID, &invite.Email, &invite.RoleID, &invite.RoleName,
		&invite.TokenHash, &invite.CreatedBy, &createdAt, &expiresAt, &acceptedAt, &invite.AcceptedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan invite: %w", err)
	}
	if invite.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if invite.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return nil, err
	}
	if invite.AcceptedAt, err = parseNullableTime(acceptedAt); err != nil {
		return nil, err
	}
	return &invite, nil
}

// GetInviteByTokenHash loads an invite by its token hash.
func (s *Store) GetInviteByTokenHash(ctx context.Context, hash []byte) (*Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites i
		LEFT JOIN roles r ON r.id = i.role_id WHERE i.token_hash = ?`, hash))
}

// GetInviteByID loads an invite by id.
func (s *Store) GetInviteByID(ctx context.Context, id string) (*Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites i
		LEFT JOIN roles r ON r.id = i.role_id WHERE i.id = ?`, id))
}

// ListPendingInvites returns unaccepted, unexpired invites for an org.
func (s *Store) ListPendingInvites(ctx context.Context, orgID string) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteColumns+` FROM invites i
		LEFT JOIN roles r ON r.id = i.role_id
		WHERE i.org_id = ? AND i.accepted_at IS NULL AND i.expires_at > ?
		ORDER BY i.created_at DESC`, orgID, formatTime(time.Now()))
	if err != nil {
		return nil, fmt.Errorf("store: list invites: %w", err)
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		invite, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *invite)
	}
	return out, rows.Err()
}

// DeleteInvite revokes an invite.
func (s *Store) DeleteInvite(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invites WHERE id = ? AND org_id = ?`, id, orgID)
	if err != nil {
		return fmt.Errorf("store: delete invite: %w", err)
	}
	return expectOne(res, "delete invite")
}

// AcceptInvite creates a user and membership and marks the invite accepted
// atomically.
func (s *Store) AcceptInvite(ctx context.Context, inviteID string, user *User, roleID string) error {
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin accept invite: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	preferences := user.Preferences
	if preferences == "" {
		preferences = "{}"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO users
		(id, email, name, avatar_url, password_hash, mfa_enabled, email_verified, preferences, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		user.ID, user.Email, user.Name, user.AvatarURL, nullString(user.PasswordHash), user.MFAEnabled,
		user.EmailVerified, preferences, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: accept invite user: %w", err)
	}
	invite, err := scanInvite(tx.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites i
		LEFT JOIN roles r ON r.id = i.role_id WHERE i.id = ?`, inviteID))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memberships (org_id, user_id, role_id, status, created_at)
		VALUES (?,?,?, 'active', ?)`, invite.OrgID, user.ID, roleID, formatTime(now)); err != nil {
		return fmt.Errorf("store: accept invite membership: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE invites SET accepted_at = ?, accepted_by = ?
		WHERE id = ? AND accepted_at IS NULL`, formatTime(now), user.ID, inviteID)
	if err != nil {
		return fmt.Errorf("store: mark invite accepted: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit accept invite: %w", err)
	}
	return nil
}

// AddMembershipFromInvite adds an existing user to the org and marks the invite
// accepted atomically.
func (s *Store) AddMembershipFromInvite(ctx context.Context, inviteID string, userID, roleID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin add membership: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	invite, err := scanInvite(tx.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites i
		LEFT JOIN roles r ON r.id = i.role_id WHERE i.id = ?`, inviteID))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memberships (org_id, user_id, role_id, status, created_at)
		VALUES (?,?,?, 'active', ?)`, invite.OrgID, userID, roleID, formatTime(time.Now())); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: add membership: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE invites SET accepted_at = ?, accepted_by = ?
		WHERE id = ? AND accepted_at IS NULL`, formatTime(time.Now()), userID, inviteID)
	if err != nil {
		return fmt.Errorf("store: mark invite accepted: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit add membership: %w", err)
	}
	return nil
}

// ListMembers returns active members of an org.
func (s *Store) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, u.name, COALESCE(m.role_id, ''),
			COALESCE(r.name, ''), m.status, m.created_at, m.removed_at
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN roles r ON r.id = m.role_id
		WHERE m.org_id = ? AND m.status = 'active' AND m.removed_at IS NULL
		ORDER BY u.name, u.email`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list members: %w", err)
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var (
			m         Member
			createdAt string
			removedAt sql.NullString
		)
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.RoleID, &m.RoleName, &m.Status, &createdAt, &removedAt); err != nil {
			return nil, fmt.Errorf("store: scan member: %w", err)
		}
		if m.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if m.RemovedAt, err = parseNullableTime(removedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateMemberRole changes a member's role.
func (s *Store) UpdateMemberRole(ctx context.Context, orgID, userID, roleID string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE memberships SET role_id = ?
		WHERE org_id = ? AND user_id = ? AND status = 'active' AND removed_at IS NULL`,
		roleID, orgID, userID)
	if err != nil {
		return fmt.Errorf("store: update member role: %w", err)
	}
	return expectOne(res, "update member role")
}

// RemoveMember marks a membership removed.
func (s *Store) RemoveMember(ctx context.Context, orgID, userID string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE memberships SET status = 'removed', removed_at = ?
		WHERE org_id = ? AND user_id = ? AND removed_at IS NULL`,
		formatTime(time.Now()), orgID, userID)
	if err != nil {
		return fmt.Errorf("store: remove member: %w", err)
	}
	return expectOne(res, "remove member")
}

// CountMembersWithRole counts active members holding a role name.
func (s *Store) CountMembersWithRole(ctx context.Context, orgID, roleName string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.org_id = ? AND r.name = ? AND m.status = 'active' AND m.removed_at IS NULL`,
		orgID, roleName).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count role members: %w", err)
	}
	return n, nil
}

// GetRoleByID loads an org role by id.
func (s *Store) GetRoleByID(ctx context.Context, id string) (*Role, error) {
	var role Role
	var orgID sql.NullString
	var builtin int
	err := s.db.QueryRowContext(ctx, `SELECT id, org_id, name, permissions, builtin FROM roles WHERE id = ?`, id).
		Scan(&role.ID, &orgID, &role.Name, &role.Permissions, &builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get role: %w", err)
	}
	role.OrgID = orgID.String
	role.Builtin = builtin != 0
	return &role, nil
}
