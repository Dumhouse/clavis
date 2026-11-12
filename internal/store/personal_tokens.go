package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ListPersonalTokens returns a user's tokens in one org.
func (s *Store) ListPersonalTokens(ctx context.Context, userID, orgID string) ([]AccessToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+tokenColumns+` FROM access_tokens
		WHERE user_id = ? AND org_id = ? ORDER BY created_at DESC`, userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list personal tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AccessToken
	for rows.Next() {
		token, err := scanAccessToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *token)
	}
	return out, rows.Err()
}

// DeletePersonalToken revokes one of the user's tokens.
func (s *Store) DeletePersonalToken(ctx context.Context, userID, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM access_tokens
		WHERE id = ? AND user_id = ? AND org_id = ?`, id, userID, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: delete personal token: %w", err)
	}
	return expectOne(res, "delete personal token")
}
