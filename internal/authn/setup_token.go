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
	path   string
	preset string
	mu     sync.Mutex
}

// NewSetupToken returns a setup token manager rooted at path.
func NewSetupToken(path string) *SetupToken {
	return &SetupToken{path: path}
}

// SetPreset makes Ensure use token instead of a random one. The operator
// supplied it (CLAVIS_SETUP_TOKEN), so it replaces any stale file content.
func (s *SetupToken) SetPreset(token string) { s.preset = strings.TrimSpace(token) }

// Path returns the token file path.
func (s *SetupToken) Path() string { return s.path }

// Ensure creates the token file if it does not exist and returns the token.
// created reports whether a new token was generated.
func (s *SetupToken) Ensure() (token string, created bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.preset != "" {
		if b, err := os.ReadFile(s.path); err == nil && strings.TrimSpace(string(b)) == s.preset {
			return s.preset, false, nil
		}
		_ = os.Remove(s.path)
		if err := s.write(s.preset); err != nil {
			return "", false, err
		}
		return s.preset, true, nil
	}

	if b, err := os.ReadFile(s.path); err == nil {
		token = strings.TrimSpace(string(b))
		if token == "" {
			return "", false, errors.New("authn: setup token file is empty")
		}
		return token, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("authn: read setup token: %w", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", false, fmt.Errorf("authn: setup token random: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	if err := s.write(token); err != nil {
		if errors.Is(err, os.ErrExist) {
			b, readErr := os.ReadFile(s.path)
			if readErr != nil {
				return "", false, fmt.Errorf("authn: read setup token: %w", readErr)
			}
			return strings.TrimSpace(string(b)), false, nil
		}
		return "", false, err
	}
	return token, true, nil
}

// write creates the token file exclusively with 0600 permissions.
func (s *SetupToken) write(token string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("authn: create setup token dir: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return err
		}
		return fmt.Errorf("authn: create setup token: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("authn: chmod setup token: %w", err)
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		return fmt.Errorf("authn: write setup token: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("authn: sync setup token: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("authn: close setup token: %w", err)
	}
	return nil
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
