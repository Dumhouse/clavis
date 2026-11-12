package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// NotifyEvent publishes a JSON payload on the shared event channel.
func (s *Store) NotifyEvent(ctx context.Context, payload string) error {
	if s.dialect != "postgres" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `SELECT pg_notify('clavis_events', ?)`, payload); err != nil {
		return fmt.Errorf("store: notify event: %w", err)
	}
	return nil
}

// ListenEvents blocks and delivers notifications until the context ends.
func (s *Store) ListenEvents(ctx context.Context, handler func(payload string)) error {
	if s.dialect != "postgres" || s.dsn == "" {
		return nil
	}
	conn, err := pgx.Connect(ctx, s.dsn)
	if err != nil {
		return fmt.Errorf("store: listen events: %w", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, `LISTEN clavis_events`); err != nil {
		return fmt.Errorf("store: listen events: %w", err)
	}
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		handler(notification.Payload)
	}
}
