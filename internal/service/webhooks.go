package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// Webhook is an org-level outgoing webhook configuration.
type Webhook struct {
	ID            string   `json:"id"`
	URL           string   `json:"url"`
	Description   string   `json:"description,omitempty"`
	Kind          string   `json:"kind,omitempty"` // generic, slack, teams, discord
	Events        []string `json:"events,omitempty"`
	ProtectedOnly bool     `json:"protected_only,omitempty"`
	Enabled       bool     `json:"enabled"`
	Secret        []byte   `json:"secret,omitempty"`
}

// WebhookView hides the signing secret.
type WebhookView struct {
	ID            string
	URL           string
	Description   string
	Kind          string
	Events        []string
	ProtectedOnly bool
	Enabled       bool
	SecretSet     bool
}

// WebhookInput creates or updates a webhook.
type WebhookInput struct {
	URL           string
	Description   string
	Kind          string
	Events        []string
	ProtectedOnly bool
	Enabled       bool
	Secret        *string
}

// ListWebhooks returns an org's webhook configuration.
func (s *Service) ListWebhooks(ctx context.Context, actor authz.Actor, orgSlug string) ([]WebhookView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	webhooks := ParseOrgSettings(org.Settings).Webhooks
	out := make([]WebhookView, 0, len(webhooks))
	for _, hook := range webhooks {
		out = append(out, webhookView(hook))
	}
	return out, nil
}

// CreateWebhook adds a webhook and returns its one-time-complete view.
func (s *Service) CreateWebhook(ctx context.Context, actor authz.Actor, orgSlug string, in WebhookInput, meta AuditMeta) (*WebhookView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	hook, err := s.buildWebhook(org, "", in)
	if err != nil {
		return nil, err
	}
	settings := ParseOrgSettings(org.Settings)
	if len(settings.Webhooks) >= 20 {
		return nil, fmt.Errorf("%w: at most 20 webhooks are allowed", ErrValidation)
	}
	settings.Webhooks = append(settings.Webhooks, hook)
	if err := s.saveWebhooks(ctx, org, settings.Webhooks); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "webhook.create",
		audit.Resource{Type: "webhook", ID: hook.ID, Name: hook.URL}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit webhook.create failed", "webhook_id", hook.ID, "error", err)
	}
	view := webhookView(hook)
	return &view, nil
}

// UpdateWebhook edits an existing webhook.
func (s *Service) UpdateWebhook(ctx context.Context, actor authz.Actor, orgSlug, id string, in WebhookInput, meta AuditMeta) (*WebhookView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	settings := ParseOrgSettings(org.Settings)
	found := -1
	for i := range settings.Webhooks {
		if settings.Webhooks[i].ID == id {
			found = i
			break
		}
	}
	if found < 0 {
		return nil, ErrNotFound
	}
	if in.Secret == nil {
		existing := settings.Webhooks[found].Secret
		in.Secret = nil
		settings.Webhooks[found], err = s.buildWebhook(org, id, in)
		if err != nil {
			return nil, err
		}
		settings.Webhooks[found].Secret = existing
	} else {
		settings.Webhooks[found], err = s.buildWebhook(org, id, in)
		if err != nil {
			return nil, err
		}
	}
	if err := s.saveWebhooks(ctx, org, settings.Webhooks); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "webhook.update",
		audit.Resource{Type: "webhook", ID: id, Name: settings.Webhooks[found].URL}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit webhook.update failed", "webhook_id", id, "error", err)
	}
	view := webhookView(settings.Webhooks[found])
	return &view, nil
}

// DeleteWebhook removes a webhook.
func (s *Service) DeleteWebhook(ctx context.Context, actor authz.Actor, orgSlug, id string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	settings := ParseOrgSettings(org.Settings)
	out := make([]Webhook, 0, len(settings.Webhooks))
	found := false
	for _, hook := range settings.Webhooks {
		if hook.ID == id {
			found = true
			continue
		}
		out = append(out, hook)
	}
	if !found {
		return ErrNotFound
	}
	if err := s.saveWebhooks(ctx, org, out); err != nil {
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "webhook.delete",
		audit.Resource{Type: "webhook", ID: id}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit webhook.delete failed", "webhook_id", id, "error", err)
	}
	return nil
}

// ListWebhookDeliveries returns recent delivery attempts.
func (s *Service) ListWebhookDeliveries(ctx context.Context, actor authz.Actor, orgSlug string, limit int) ([]store.WebhookDelivery, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return s.Store.ListWebhookDeliveries(ctx, org.ID, limit)
}

func (s *Service) buildWebhook(org *store.Org, id string, in WebhookInput) (Webhook, error) {
	url := strings.TrimSpace(in.URL)
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return Webhook{}, fmt.Errorf("%w: webhook URL must be http(s)", ErrValidation)
	}
	events := make([]string, 0, len(in.Events))
	for _, event := range in.Events {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		events = append(events, event)
	}
	if len(events) > 50 {
		return Webhook{}, fmt.Errorf("%w: at most 50 event filters are allowed", ErrValidation)
	}
	if id == "" {
		id = uuid.NewString()
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		kind = "generic"
	}
	switch kind {
	case "generic", "slack", "teams", "discord":
	default:
		return Webhook{}, fmt.Errorf("%w: webhook kind must be generic, slack, teams, or discord", ErrValidation)
	}
	hook := Webhook{
		ID: id, URL: url, Description: strings.TrimSpace(in.Description), Kind: kind,
		Events: events, ProtectedOnly: in.ProtectedOnly, Enabled: in.Enabled,
	}
	if in.Secret != nil && *in.Secret != "" {
		ciphertext, err := s.encryptOrgSecret(org, hook.ID, "webhook_secret", *in.Secret)
		if err != nil {
			return Webhook{}, err
		}
		hook.Secret = ciphertext
	}
	return hook, nil
}

func (s *Service) saveWebhooks(ctx context.Context, org *store.Org, hooks []Webhook) error {
	raw := map[string]any{}
	if org.Settings != "" {
		_ = json.Unmarshal([]byte(org.Settings), &raw)
	}
	raw["webhooks"] = hooks
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return s.Store.UpdateOrgSettings(ctx, org.ID, string(encoded))
}

func (s *Service) encryptOrgSecret(org *store.Org, itemID, field, secret string) ([]byte, error) {
	orgKey, err := s.Keyring.UnwrapOrgKey(org.ID, org.WrappedOrgKey)
	if err != nil {
		return nil, err
	}
	defer orgKey.Zero()
	aad := crypto.BindAAD(org.ID, "", "", itemID, field, 1)
	return orgKey.Encrypt(uint32(org.KeyVersion), crypto.Secret(secret), aad)
}

func (s *Service) decryptOrgSecret(org *store.Org, itemID, field string, ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	orgKey, err := s.Keyring.UnwrapOrgKey(org.ID, org.WrappedOrgKey)
	if err != nil {
		return "", err
	}
	defer orgKey.Zero()
	aad := crypto.BindAAD(org.ID, "", "", itemID, field, 1)
	plaintext, _, err := orgKey.Decrypt(ciphertext, aad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func webhookView(hook Webhook) WebhookView {
	kind := hook.Kind
	if kind == "" {
		kind = "generic"
	}
	return WebhookView{
		ID: hook.ID, URL: hook.URL, Description: hook.Description, Kind: kind,
		Events: hook.Events, ProtectedOnly: hook.ProtectedOnly, Enabled: hook.Enabled,
		SecretSet: len(hook.Secret) > 0,
	}
}

// runWebhookWorker delivers queued audit events to matching webhooks.
func (s *Service) runWebhookWorker() {
	for event := range s.webhookQueue {
		s.deliverWebhooks(context.Background(), event)
	}
}

func (s *Service) deliverWebhooks(ctx context.Context, event *audit.Event) {
	org, err := s.Store.GetOrgByID(ctx, event.OrgID)
	if err != nil {
		return
	}
	hooks := ParseOrgSettings(org.Settings).Webhooks
	for _, hook := range hooks {
		if !hook.Enabled || !webhookMatches(hook, event.Action) {
			continue
		}
		if hook.ProtectedOnly && !s.eventIsProtected(ctx, event) {
			continue
		}
		secret, err := s.decryptOrgSecret(org, hook.ID, "webhook_secret", hook.Secret)
		if err != nil {
			s.Logger.Warn("webhook secret decrypt failed", "webhook_id", hook.ID, "error", err)
			continue
		}
		s.deliverOneWebhook(ctx, org, hook, secret, event)
	}
}

func webhookMatches(hook Webhook, action string) bool {
	if len(hook.Events) == 0 {
		return true
	}
	for _, filter := range hook.Events {
		if filter == "*" || filter == action {
			return true
		}
		if strings.HasSuffix(filter, "*") && strings.HasPrefix(action, strings.TrimSuffix(filter, "*")) {
			return true
		}
	}
	return false
}

func (s *Service) deliverOneWebhook(ctx context.Context, org *store.Org, hook Webhook, secret string, event *audit.Event) {
	payload, err := webhookPayload(hook, event)
	if err != nil {
		return
	}
	delivery := &store.WebhookDelivery{
		OrgID: org.ID, WebhookID: hook.ID, EventID: event.ID,
		EventType: event.Action, URL: hook.URL, Status: "pending", Attempts: 0,
		CreatedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateWebhookDelivery(ctx, delivery); err != nil {
		s.Logger.Warn("create webhook delivery failed", "webhook_id", hook.ID, "error", err)
		return
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	backoff := time.Second
	var lastErr string
	for attempt := 1; attempt <= 5; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(payload))
		if err == nil {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("User-Agent", "clavis-webhook/1")
			request.Header.Set("X-Clavis-Event", event.Action)
			request.Header.Set("X-Clavis-Delivery", delivery.ID)
			if secret != "" {
				mac := hmac.New(sha256.New, []byte(secret))
				mac.Write(payload)
				request.Header.Set("X-Clavis-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			}
			var response *http.Response
			response, err = client.Do(request)
			if err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
				_ = response.Body.Close()
				delivery.ResponseStatus = response.StatusCode
				if response.StatusCode >= 200 && response.StatusCode < 300 {
					delivery.Status = "success"
					delivery.Attempts = attempt
					delivery.LastError = ""
					now := s.Now().UTC()
					delivery.DeliveredAt = &now
					_ = s.Store.UpdateWebhookDelivery(ctx, delivery)
					return
				}
				lastErr = fmt.Sprintf("destination returned %d", response.StatusCode)
			}
		}
		if err != nil {
			lastErr = err.Error()
		}
		delivery.Attempts = attempt
		delivery.Status = "retrying"
		delivery.LastError = lastErr
		_ = s.Store.UpdateWebhookDelivery(ctx, delivery)
		if attempt < 5 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
		}
	}
	delivery.Status = "failed"
	_ = s.Store.UpdateWebhookDelivery(ctx, delivery)
}

func (s *Service) eventIsProtected(ctx context.Context, event *audit.Event) bool {
	if event.Resource.EnvID == "" {
		return false
	}
	env, err := s.Store.GetEnvironmentByID(ctx, event.Resource.EnvID)
	if err != nil {
		return false
	}
	return env.IsProtected
}

func webhookPayload(hook Webhook, event *audit.Event) ([]byte, error) {
	summary := "Clavis " + event.Action
	if event.Resource.Name != "" {
		summary += ": " + event.Resource.Name
	}
	if event.Actor.Name != "" {
		summary += " by " + event.Actor.Name
	}
	if event.Details.Environment != "" {
		summary += " in " + event.Details.Environment
	}
	switch hook.Kind {
	case "slack":
		return json.Marshal(map[string]any{"text": summary})
	case "discord":
		return json.Marshal(map[string]any{"content": summary})
	case "teams":
		return json.Marshal(map[string]any{
			"@type":    "MessageCard",
			"@context": "https://schema.org/extensions",
			"summary":  summary,
			"text":     summary,
		})
	default:
		return json.Marshal(map[string]any{"event": event})
	}
}
