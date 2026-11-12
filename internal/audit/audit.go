// Package audit implements Clavis' append-only, per-organization
// hash-chained audit log.
//
// Events deliberately use typed details instead of a free-form metadata map,
// so the schema cannot carry a secret value even by mistake.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Event is one audit log record.
type Event struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Seq       int64     `json:"seq"`
	Actor     Actor     `json:"actor"`
	Action    string    `json:"action"`
	Resource  Resource  `json:"resource"`
	Details   Details   `json:"details,omitempty"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	PrevHash  string    `json:"prev_hash"`
	Hash      string    `json:"hash"`
}

// Actor identifies who performed an action.
type Actor struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

// Resource identifies what was acted on.
type Resource struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	EnvID     string `json:"env_id,omitempty"`
}

// Details holds the only additional data an event may carry. It has no
// free-form or value-bearing fields by design.
type Details struct {
	KeyBlindIndex string   `json:"key_blind_index,omitempty"`
	Path          string   `json:"path,omitempty"`
	Environment   string   `json:"environment,omitempty"`
	Version       int      `json:"version,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Channel       string   `json:"channel,omitempty"`
	Command       string   `json:"command,omitempty"`
	ChangedKeys   []string `json:"changed_keys,omitempty"` // blind indexes only
	Count         int      `json:"count,omitempty"`
	RequestID     string   `json:"request_id,omitempty"`
}

// GenisisHash is the prev_hash of the first event in every org chain.
var genesisHash = strings.Repeat("0", sha256.Size*2)

// MaxFieldLength bounds free-text fields so an audit event cannot be used to
// smuggle a file or blob.
const MaxFieldLength = 4096

// ErrInvalidEvent is returned for events that violate the schema.
var ErrInvalidEvent = errors.New("audit: invalid event")

// Validate checks the structural invariants of an event before sealing.
func (e *Event) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("%w: missing id", ErrInvalidEvent)
	}
	if e.OrgID == "" {
		return fmt.Errorf("%w: missing org_id", ErrInvalidEvent)
	}
	if e.Actor.ID == "" || e.Actor.Kind == "" {
		return fmt.Errorf("%w: missing actor", ErrInvalidEvent)
	}
	if e.Action == "" {
		return fmt.Errorf("%w: missing action", ErrInvalidEvent)
	}
	if e.Resource.Type == "" {
		return fmt.Errorf("%w: missing resource type", ErrInvalidEvent)
	}
	if e.CreatedAt.IsZero() {
		return fmt.Errorf("%w: missing created_at", ErrInvalidEvent)
	}
	for name, value := range map[string]string{
		"action":        e.Action,
		"ip":            e.IP,
		"user agent":    e.UserAgent,
		"reason":        e.Details.Reason,
		"command":       e.Details.Command,
		"path":          e.Details.Path,
		"channel":       e.Details.Channel,
		"resource name": e.Resource.Name,
	} {
		if len(value) > MaxFieldLength {
			return fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidEvent, name, MaxFieldLength)
		}
	}
	if len(e.Details.ChangedKeys) > 1000 {
		return fmt.Errorf("%w: too many changed keys", ErrInvalidEvent)
	}
	return nil
}

// Canonical returns the deterministic JSON encoding that is signed by Hash.
func (e *Event) Canonical() ([]byte, error) {
	// encoding/json sorts map keys, and the struct has no maps with
	// user-controlled ordering, so this encoding is stable.
	return json.Marshal(struct {
		ID        string    `json:"id"`
		OrgID     string    `json:"org_id"`
		Seq       int64     `json:"seq"`
		Actor     Actor     `json:"actor"`
		Action    string    `json:"action"`
		Resource  Resource  `json:"resource"`
		Details   Details   `json:"details,omitempty"`
		IP        string    `json:"ip,omitempty"`
		UserAgent string    `json:"user_agent,omitempty"`
		CreatedAt time.Time `json:"created_at"`
	}{
		ID: e.ID, OrgID: e.OrgID, Seq: e.Seq, Actor: e.Actor, Action: e.Action,
		Resource: e.Resource, Details: e.Details, IP: e.IP, UserAgent: e.UserAgent,
		CreatedAt: e.CreatedAt,
	})
}

// HashEvent computes SHA-256(prev_hash || canonical(event)).
func HashEvent(prevHash string, event *Event) (string, error) {
	canonical, err := event.Canonical()
	if err != nil {
		return "", fmt.Errorf("audit: canonicalize: %w", err)
	}
	prev, err := hex.DecodeString(prevHash)
	if err != nil || len(prev) != sha256.Size {
		return "", fmt.Errorf("audit: invalid prev_hash")
	}
	h := sha256.New()
	h.Write(prev)
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Seal assigns seq, prev_hash, and hash. prev is the previous event in the
// same org chain, or nil for the first event.
func Seal(prev *Event, event *Event) error {
	if event == nil {
		return fmt.Errorf("%w: nil event", ErrInvalidEvent)
	}
	if err := event.Validate(); err != nil {
		return err
	}
	switch {
	case prev == nil:
		event.Seq = 1
		event.PrevHash = genesisHash
	case prev.OrgID != event.OrgID:
		return fmt.Errorf("%w: previous event belongs to a different org", ErrInvalidEvent)
	default:
		event.Seq = prev.Seq + 1
		event.PrevHash = prev.Hash
	}
	hash, err := HashEvent(event.PrevHash, event)
	if err != nil {
		return err
	}
	event.Hash = hash
	return nil
}

// Verify recomputes an entire org chain and reports the first corrupt event.
// events must be ordered by seq ascending.
func Verify(events []Event) error {
	prevHash := genesisHash
	for i := range events {
		ev := &events[i]
		if ev.Seq != int64(i+1) {
			return fmt.Errorf("audit: event %s has seq %d, want %d", ev.ID, ev.Seq, i+1)
		}
		if ev.PrevHash != prevHash {
			return fmt.Errorf("audit: event %s has broken prev_hash at seq %d", ev.ID, ev.Seq)
		}
		if err := ev.Validate(); err != nil {
			// Validation uses CreatedAt and other fields, which are covered by
			// the hash; a change here is also a chain break.
			return fmt.Errorf("audit: event %s invalid: %w", ev.ID, err)
		}
		want, err := HashEvent(ev.PrevHash, ev)
		if err != nil {
			return err
		}
		if ev.Hash != want {
			return fmt.Errorf("audit: event %s has invalid hash at seq %d", ev.ID, ev.Seq)
		}
		prevHash = ev.Hash
	}
	return nil
}
