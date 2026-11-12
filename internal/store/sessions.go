package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a server-side browser session. Only hashes of the session and
// CSRF tokens are stored.
type Session struct {
	ID          string
	UserID      string
	TokenHash   []byte
	CSRFHash    []byte
	IP          string
	UserAgent   string
	MFAVerified bool
	CreatedAt   time.Time
	LastSeenAt  time.Time
	// SeenBefore is the pre-request last-seen value, used by org policy checks
	// after this request touches the session.
	SeenBefore time.Time
	ExpiresAt  time.Time
}

// CreateSession inserts a session.
func (s *Store) CreateSession(ctx context.Context, session *Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
		(id, user_id, token_hash, csrf_hash, ip, user_agent, mfa_verified, created_at, last_seen_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		session.ID, session.UserID, session.TokenHash, session.CSRFHash, session.IP, session.UserAgent,
		session.MFAVerified, formatTime(session.CreatedAt), formatTime(session.LastSeenAt), formatTime(session.ExpiresAt))
	if err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

// GetSessionByTokenHash returns a session and its user.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (*Session, *User, error) {
	var (
		session                            Session
		user                               User
		mfaVerified, userMFA, userVerified int
		createdAt, lastSeenAt, expiresAt   string
		userCreatedAt, userUpdatedAt       string
		passwordHash                       sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT
			s.id, s.user_id, s.token_hash, COALESCE(s.csrf_hash, ?), s.ip, s.user_agent,
			s.mfa_verified, s.created_at, s.last_seen_at, s.expires_at,
			u.id, u.email, u.name, u.avatar_url, u.password_hash, u.mfa_enabled,
			u.mfa_secret_ciphertext, u.email_verified, u.preferences, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, []byte{}, tokenHash).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.CSRFHash, &session.IP, &session.UserAgent,
		&mfaVerified, &createdAt, &lastSeenAt, &expiresAt,
		&user.ID, &user.Email, &user.Name, &user.AvatarURL, &passwordHash, &userMFA,
		&user.MFASecretCiphertext, &userVerified, &user.Preferences, &userCreatedAt, &userUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("store: get session: %w", err)
	}
	session.MFAVerified = mfaVerified != 0
	user.MFAEnabled = userMFA != 0
	user.EmailVerified = userVerified != 0
	user.PasswordHash = passwordHash.String

	for _, parse := range []struct {
		src string
		dst *time.Time
	}{
		{createdAt, &session.CreatedAt},
		{lastSeenAt, &session.LastSeenAt},
		{expiresAt, &session.ExpiresAt},
		{userCreatedAt, &user.CreatedAt},
		{userUpdatedAt, &user.UpdatedAt},
	} {
		t, err := parseTime(parse.src)
		if err != nil {
			return nil, nil, err
		}
		*parse.dst = t
	}
	return &session, &user, nil
}

// TouchSession updates last_seen_at.
func (s *Store) TouchSession(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, formatTime(at), id)
	if err != nil {
		return fmt.Errorf("store: touch session: %w", err)
	}
	return expectOne(res, "touch session")
}

// DeleteSession revokes one session.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return expectOne(res, "delete session")
}

// DeleteUserSessions revokes every session for a user and returns the count.
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("store: delete user sessions: %w", err)
	}
	return res.RowsAffected()
}

// ListSessionsForUser returns active sessions without token material.
func (s *Store) ListSessionsForUser(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, ip, user_agent, mfa_verified, created_at, last_seen_at, expires_at
		FROM sessions WHERE user_id = ? ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var (
			session                          Session
			mfa                              int
			createdAt, lastSeenAt, expiresAt string
		)
		if err := rows.Scan(&session.ID, &session.UserID, &session.IP, &session.UserAgent, &mfa,
			&createdAt, &lastSeenAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("store: scan session: %w", err)
		}
		session.MFAVerified = mfa != 0
		if session.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if session.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
			return nil, err
		}
		if session.ExpiresAt, err = parseTime(expiresAt); err != nil {
			return nil, err
		}
		out = append(out, session)
	}
	return out, rows.Err()
}

// DeleteExpiredSessions removes sessions past their absolute expiry.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("store: delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}

// CredentialActive reports whether a user's session ("session") or personal
// access token ("pat") still exists and has not expired. Delegated agent
// sessions use it so that logging out or revoking a token ends them at once.
func (s *Store) CredentialActive(ctx context.Context, kind, id, userID string, now time.Time) (bool, error) {
	var query string
	switch kind {
	case "session":
		query = `SELECT expires_at FROM sessions WHERE id = ? AND user_id = ?`
	case "pat":
		query = `SELECT expires_at FROM access_tokens WHERE id = ? AND user_id = ?`
	default:
		return false, nil
	}
	var expiresAt sql.NullString
	err := s.db.QueryRowContext(ctx, query, id, userID).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: check credential: %w", err)
	}
	expires, err := parseNullableTime(expiresAt)
	if err != nil {
		return false, err
	}
	return expires == nil || now.Before(*expires), nil
}
