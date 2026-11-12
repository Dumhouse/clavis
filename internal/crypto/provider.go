package crypto

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Provider supplies the root key (KEK). Root keys are never stored in the
// database.
type Provider interface {
	Name() string
	Load(ctx context.Context) (Key, error)
	Close() error
}

// FileProvider reads a root key from a file, generating one with 0600
// permissions on first use.
type FileProvider struct {
	path     string
	generate bool
}

// NewFileProvider returns a root key provider backed by path.
func NewFileProvider(path string, generate bool) *FileProvider {
	return &FileProvider{path: path, generate: generate}
}

// Name implements Provider.
func (p *FileProvider) Name() string { return "file" }

// Load implements Provider.
func (p *FileProvider) Load(ctx context.Context) (Key, error) {
	if err := ctx.Err(); err != nil {
		return Key{}, err
	}
	b, err := os.ReadFile(p.path)
	if err == nil {
		return p.checkAndDecode(b)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Key{}, fmt.Errorf("crypto: read root key %s: %w", p.path, err)
	}
	if !p.generate {
		return Key{}, fmt.Errorf("crypto: root key %s does not exist; generate one with `clavis admin rotate-root-key --init`", p.path)
	}
	return p.generateKey()
}

func (p *FileProvider) checkAndDecode(b []byte) (Key, error) {
	info, err := os.Stat(p.path)
	if err != nil {
		return Key{}, fmt.Errorf("crypto: stat root key: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Key{}, fmt.Errorf("crypto: root key %s has insecure permissions %o (want 0600); run: chmod 600 %s", p.path, info.Mode().Perm(), p.path)
	}
	return decodeKey(strings.TrimSpace(string(b)))
}

func (p *FileProvider) generateKey() (Key, error) {
	key, err := GenerateKey()
	if err != nil {
		return Key{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return Key{}, fmt.Errorf("crypto: create root key directory: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(key[:]) + "\n"
	f, err := os.OpenFile(p.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		// Another process won the race; read its key instead.
		if errors.Is(err, os.ErrExist) {
			b, readErr := os.ReadFile(p.path)
			if readErr != nil {
				return Key{}, fmt.Errorf("crypto: read root key: %w", readErr)
			}
			return p.checkAndDecode(b)
		}
		return Key{}, fmt.Errorf("crypto: create root key: %w", err)
	}
	defer f.Close()

	// Enforce 0600 even if the process umask would have widened it.
	if err := f.Chmod(0o600); err != nil {
		return Key{}, fmt.Errorf("crypto: chmod root key: %w", err)
	}
	if _, err := f.WriteString(encoded); err != nil {
		return Key{}, fmt.Errorf("crypto: write root key: %w", err)
	}
	if err := f.Sync(); err != nil {
		return Key{}, fmt.Errorf("crypto: sync root key: %w", err)
	}
	return key, nil
}

// Save writes a new root key atomically with 0600 permissions. Use only while
// the server is stopped.
func (p *FileProvider) Save(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return fmt.Errorf("crypto: create root key directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(p.path), ".root-key-*")
	if err != nil {
		return fmt.Errorf("crypto: create temporary root key: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("crypto: chmod temporary root key: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(key[:]) + "\n"
	if _, err := temp.WriteString(encoded); err != nil {
		_ = temp.Close()
		return fmt.Errorf("crypto: write temporary root key: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("crypto: sync temporary root key: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("crypto: close temporary root key: %w", err)
	}
	if err := os.Rename(tempName, p.path); err != nil {
		return fmt.Errorf("crypto: replace root key: %w", err)
	}
	if dir, err := os.Open(filepath.Dir(p.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// Close implements Provider.
func (p *FileProvider) Close() error { return nil }

// EnvProvider reads the root key from an environment variable containing a
// base64 (standard or raw URL) encoded 32-byte key.
type EnvProvider struct {
	varName string
}

// NewEnvProvider returns a provider backed by an environment variable.
func NewEnvProvider(varName string) *EnvProvider { return &EnvProvider{varName: varName} }

// Name implements Provider.
func (p *EnvProvider) Name() string { return "env" }

// Load implements Provider.
func (p *EnvProvider) Load(ctx context.Context) (Key, error) {
	if err := ctx.Err(); err != nil {
		return Key{}, err
	}
	v, ok := os.LookupEnv(p.varName)
	if !ok || v == "" {
		return Key{}, fmt.Errorf("crypto: environment variable %s is not set", p.varName)
	}
	return decodeKey(v)
}

// Close implements Provider.
func (p *EnvProvider) Close() error { return nil }

func decodeKey(s string) (Key, error) {
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		b, err := enc.DecodeString(s)
		if err == nil {
			return NewKeyFromBytes(b)
		}
	}
	return Key{}, errors.New("crypto: root key is not valid base64")
}
