package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CredentialStore persists CLI tokens. Prefer the OS keychain; fall back to a
// 0600 file only when no keychain helper is available.
type CredentialStore interface {
	Get(server string) (string, error)
	Set(server, token string) error
	Delete(server string) error
	Description() string
}

// ErrNoCredential means the server has no saved token.
var ErrNoCredential = errors.New("cli: no saved credential")

func credentialStore() CredentialStore {
	if path := os.Getenv("CLAVIS_CREDENTIAL_FILE"); path != "" {
		return &fileCredentialStore{path: path}
	}
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("security"); err == nil {
			return &keychainCredentialStore{}
		}
	}
	if _, err := exec.LookPath("secret-tool"); err == nil && runtime.GOOS == "linux" {
		return &secretToolCredentialStore{}
	}
	path, err := defaultCredentialPath()
	if err != nil {
		return &fileCredentialStore{path: filepath.Join(os.TempDir(), "clavis-credentials.json")}
	}
	return &fileCredentialStore{path: path}
}

func defaultCredentialPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clavis", "credentials.json"), nil
}

type keychainCredentialStore struct{}

func (k *keychainCredentialStore) Description() string { return "macOS Keychain" }

func (k *keychainCredentialStore) Get(server string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", "clavis", "-a", server, "-w").Output()
	if err != nil {
		return "", ErrNoCredential
	}
	return strings.TrimSpace(string(out)), nil
}

func (k *keychainCredentialStore) Set(server, token string) error {
	_ = exec.Command("security", "delete-generic-password", "-s", "clavis", "-a", server).Run()
	if out, err := exec.Command("security", "add-generic-password", "-s", "clavis", "-a", server, "-w", token).CombinedOutput(); err != nil {
		return fmt.Errorf("cli: save token in Keychain: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (k *keychainCredentialStore) Delete(server string) error {
	_ = exec.Command("security", "delete-generic-password", "-s", "clavis", "-a", server).Run()
	return nil
}

type secretToolCredentialStore struct{}

func (s *secretToolCredentialStore) Description() string { return "Secret Service" }

func (s *secretToolCredentialStore) Get(server string) (string, error) {
	out, err := exec.Command("secret-tool", "lookup", "service", "clavis", "server", server).Output()
	if err != nil {
		return "", ErrNoCredential
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *secretToolCredentialStore) Set(server, token string) error {
	cmd := exec.Command("secret-tool", "store", "--label", "Clavis CLI token", "service", "clavis", "server", server)
	cmd.Stdin = strings.NewReader(token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cli: save token in Secret Service: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *secretToolCredentialStore) Delete(server string) error {
	_ = exec.Command("secret-tool", "clear", "service", "clavis", "server", server).Run()
	return nil
}

type fileCredentialStore struct {
	path string
}

func (f *fileCredentialStore) Description() string { return "file " + f.path }

func (f *fileCredentialStore) load() (map[string]string, error) {
	b, err := os.ReadFile(f.path) //nolint:gosec // G703: credentials file path is chosen by the CLI, not by remote input
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("cli: parse credentials: %w", err)
	}
	return out, nil
}

func (f *fileCredentialStore) save(tokens map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *fileCredentialStore) Get(server string) (string, error) {
	tokens, err := f.load()
	if err != nil {
		return "", err
	}
	token := tokens[server]
	if token == "" {
		return "", ErrNoCredential
	}
	return token, nil
}

func (f *fileCredentialStore) Set(server, token string) error {
	tokens, err := f.load()
	if err != nil {
		return err
	}
	tokens[server] = token
	return f.save(tokens)
}

func (f *fileCredentialStore) Delete(server string) error {
	tokens, err := f.load()
	if err != nil {
		return err
	}
	delete(tokens, server)
	return f.save(tokens)
}
