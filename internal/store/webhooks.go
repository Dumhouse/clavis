package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// WebhookDelivery records one delivery attempt history row.
type WebhookDelivery struct {
	ID             string
	OrgID          string
	WebhookID      string
	EventID        string
	EventType      string
	URL            string
	Status         string
	Attempts       int
	ResponseStatus int
	LastError      string
	CreatedAt      time.Time
	DeliveredAt    *time.Time
}

// CreateWebhookDelivery inserts a pending delivery.
func (s *Store) CreateWebhookDelivery(ctx context.Context, delivery *WebhookDelivery) error {
	if delivery.ID == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = time.Now().UTC()
	}
	if delivery.Status == "" {
		delivery.Status = "pending"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO webhook_deliveries
		(id, org_id, webhook_id, event_id, event_type, url, status, attempts,
		 response_status, last_error, created_at, delivered_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		delivery.ID, delivery.OrgID, delivery.WebhookID, delivery.EventID, delivery.EventType,
		delivery.URL, delivery.Status, delivery.Attempts, delivery.ResponseStatus,
		delivery.LastError, formatTime(delivery.CreatedAt), nullableTime(delivery.DeliveredAt))
	if err != nil {
		return fmt.Errorf("store: create webhook delivery: %w", err)
	}
	return nil
}

// UpdateWebhookDelivery updates status and attempt metadata.
func (s *Store) UpdateWebhookDelivery(ctx context.Context, delivery *WebhookDelivery) error {
	res, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries
		SET status = ?, attempts = ?, response_status = ?, last_error = ?, delivered_at = ?
		WHERE id = ?`,
		delivery.Status, delivery.Attempts, delivery.ResponseStatus, delivery.LastError,
		nullableTime(delivery.DeliveredAt), delivery.ID)
	if err != nil {
		return fmt.Errorf("store: update webhook delivery: %w", err)
	}
	return expectOne(res, "update webhook delivery")
}

// ListWebhookDeliveries returns recent deliveries for an org.
func (s *Store) ListWebhookDeliveries(ctx context.Context, orgID string, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, org_id, webhook_id, event_id, event_type, url,
			status, attempts, COALESCE(response_status, 0), last_error, created_at, delivered_at
		FROM webhook_deliveries WHERE org_id = ? ORDER BY created_at DESC LIMIT ?`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list webhook deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WebhookDelivery
	for rows.Next() {
		var delivery WebhookDelivery
		var createdAt string
		var deliveredAt sql.NullString
		if err := rows.Scan(&delivery.ID, &delivery.OrgID, &delivery.WebhookID, &delivery.EventID,
			&delivery.EventType, &delivery.URL, &delivery.Status, &delivery.Attempts,
			&delivery.ResponseStatus, &delivery.LastError, &createdAt, &deliveredAt); err != nil {
			return nil, fmt.Errorf("store: scan webhook delivery: %w", err)
		}
		if delivery.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if delivery.DeliveredAt, err = parseNullableTime(deliveredAt); err != nil {
			return nil, err
		}
		out = append(out, delivery)
	}
	return out, rows.Err()
}

// GetWebhookDelivery is used by tests and tooling.
func (s *Store) GetWebhookDelivery(ctx context.Context, orgID, id string) (*WebhookDelivery, error) {
	var delivery WebhookDelivery
	var createdAt string
	var deliveredAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, org_id, webhook_id, event_id, event_type, url,
			status, attempts, COALESCE(response_status, 0), last_error, created_at, delivered_at
		FROM webhook_deliveries WHERE id = ? AND org_id = ?`, id, orgID).
		Scan(&delivery.ID, &delivery.OrgID, &delivery.WebhookID, &delivery.EventID, &delivery.EventType,
			&delivery.URL, &delivery.Status, &delivery.Attempts, &delivery.ResponseStatus,
			&delivery.LastError, &createdAt, &deliveredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get webhook delivery: %w", err)
	}
	if delivery.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if delivery.DeliveredAt, err = parseNullableTime(deliveredAt); err != nil {
		return nil, err
	}
	return &delivery, nil
}
