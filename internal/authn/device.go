package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	// ErrDevicePending means the browser has not approved the code yet.
	ErrDevicePending = errors.New("authn: device authorization pending")
	// ErrDeviceNotFound is returned for an unknown or expired device code.
	ErrDeviceNotFound = errors.New("authn: device code not found")
	// ErrDeviceDenied means a human rejected the request.
	ErrDeviceDenied = errors.New("authn: device authorization denied")
	// ErrDeviceConsumed means the token was already collected.
	ErrDeviceConsumed = errors.New("authn: device code already used")
)

// DeviceGrant is a pending CLI login.
type DeviceGrant struct {
	DeviceCode string
	UserCode   string
	ExpiresAt  time.Time
	Interval   time.Duration
}

// DeviceManager tracks short-lived device authorization requests in memory.
// Multi-replica deployments will persist these in M3; a single binary is the
// M1 deployment mode.
type DeviceManager struct {
	mu       sync.Mutex
	ttl      time.Duration
	interval time.Duration
	now      func() time.Time
	byDevice map[string]*deviceEntry
	byUser   map[string]string
}

type deviceEntry struct {
	grant    DeviceGrant
	userID   string
	denied   bool
	consumed bool
}

const deviceAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewDeviceManager returns a manager with a 10-minute lifetime and 2-second
// polling interval.
func NewDeviceManager(now func() time.Time) *DeviceManager {
	if now == nil {
		now = time.Now
	}
	return &DeviceManager{
		ttl:      10 * time.Minute,
		interval: 2 * time.Second,
		now:      now,
		byDevice: make(map[string]*deviceEntry),
		byUser:   make(map[string]string),
	}
}

// Create starts a device authorization request.
func (m *DeviceManager) Create() (*DeviceGrant, error) {
	deviceRaw := make([]byte, 32)
	if _, err := rand.Read(deviceRaw); err != nil {
		return nil, fmt.Errorf("authn: device code: %w", err)
	}
	deviceCode := base64.RawURLEncoding.EncodeToString(deviceRaw)

	userCode, err := newUserCode()
	if err != nil {
		return nil, err
	}
	grant := DeviceGrant{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		ExpiresAt:  m.now().UTC().Add(m.ttl),
		Interval:   m.interval,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	m.byDevice[deviceCode] = &deviceEntry{grant: grant}
	m.byUser[normalizeUserCode(userCode)] = deviceCode
	return &grant, nil
}

// Approve links a user to a pending code.
func (m *DeviceManager) Approve(userCode, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	deviceCode, ok := m.byUser[normalizeUserCode(userCode)]
	if !ok {
		return ErrDeviceNotFound
	}
	entry, ok := m.byDevice[deviceCode]
	if !ok || m.now().UTC().After(entry.grant.ExpiresAt) {
		return ErrDeviceNotFound
	}
	if entry.consumed {
		return ErrDeviceConsumed
	}
	entry.userID = userID
	return nil
}

// Deny rejects a pending code.
func (m *DeviceManager) Deny(userCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	deviceCode, ok := m.byUser[normalizeUserCode(userCode)]
	if !ok {
		return ErrDeviceNotFound
	}
	if entry, ok := m.byDevice[deviceCode]; ok {
		entry.denied = true
	}
	return nil
}

// Exchange consumes an approved code and returns the approving user's ID.
func (m *DeviceManager) Exchange(deviceCode string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.byDevice[deviceCode]
	if !ok || m.now().UTC().After(entry.grant.ExpiresAt) {
		return "", ErrDeviceNotFound
	}
	if entry.denied {
		return "", ErrDeviceDenied
	}
	if entry.consumed {
		return "", ErrDeviceConsumed
	}
	if entry.userID == "" {
		return "", ErrDevicePending
	}
	entry.consumed = true
	return entry.userID, nil
}

func (m *DeviceManager) pruneLocked() {
	now := m.now().UTC()
	for code, entry := range m.byDevice {
		if now.After(entry.grant.ExpiresAt) {
			delete(m.byDevice, code)
			delete(m.byUser, normalizeUserCode(entry.grant.UserCode))
		}
	}
}

// NormalizeUserCode strips spaces and hyphens and uppercases a code.
func NormalizeUserCode(code string) string { return normalizeUserCode(code) }

func normalizeUserCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func newUserCode() (string, error) {
	const length = 8
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("authn: user code: %w", err)
	}
	out := make([]byte, length)
	for i := range b {
		out[i] = deviceAlphabet[int(b[i])%len(deviceAlphabet)]
	}
	return string(out[:4]) + "-" + string(out[4:]), nil
}

// ConstantTimeCodeEqual compares two user codes after normalization.
func ConstantTimeCodeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(normalizeUserCode(a)), []byte(normalizeUserCode(b))) == 1
}
