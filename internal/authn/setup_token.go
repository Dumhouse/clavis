package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SetupToken mediates first-run setup. The token is written to a 0600 file and
// printed once to the server log. Whoever can read either may claim the
// instance, so setup closes permanently after the first owner exists.
type SetupToken struct {
	path string
	mu   sync.Mutex
}

// NewSetupToken returns a setup token manager rooted at path.
func NewSetupToken(path string) *SetupToken {
	return &SetupToken{path: path}
}

// Path returns the token file path.
func (s *SetupToken) Path() string { return s.path }

// Ensure creates the token file if it does not exist and returns the token.
// created reports whether a new token was generated.
func (s *SetupToken) Ensure() (token string, created bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if b, err := os.ReadFile(s.path); err == nil {
		token = strings.TrimSpace(string(b))
		if token == "" {
			return "", false, errors.New("authn: setup token file is empty")
		}
		return token, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("authn: read setup token: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return "", false, fmt.Errorf("authn: create setup token dir: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", false, fmt.Errorf("authn: setup token random: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)

	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			b, readErr := os.ReadFile(s.path)
			if readErr != nil {
				return "", false, fmt.Errorf("authn: read setup token: %w", readErr)
			}
			return strings.TrimSpace(string(b)), false, nil
		}
		return "", false, fmt.Errorf("authn: create setup token: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0o600); err != nil {
		return "", false, fmt.Errorf("authn: chmod setup token: %w", err)
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		return "", false, fmt.Errorf("authn: write setup token: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", false, fmt.Errorf("authn: sync setup token: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", false, fmt.Errorf("authn: close setup token: %w", err)
	}
	return token, true, nil
}

// Verify compares candidate to the stored token in constant time.
func (s *SetupToken) Verify(candidate string) bool {
	if candidate == "" {
		return false
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return false
	}
	stored := strings.TrimSpace(string(b))
	return subtle.ConstantTimeCompare([]byte(stored), []byte(candidate)) == 1
}

// Consume deletes the token file. It is idempotent.
func (s *SetupToken) Consume() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("authn: remove setup token: %w", err)
	}
	return nil
}
