package cli

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const cacheTTL = 24 * time.Hour

type cachedSecrets struct {
	CreatedAt time.Time    `json:"created_at"`
	Secrets   []secretJSON `json:"secrets"`
}

func cacheScope(resolved *resolvedContext) string {
	return resolved.Server + "|" + resolved.Org + "|" + resolved.Project + "|" + resolved.Env
}

func cacheKey(resolved *resolvedContext) ([]byte, error) {
	store := credentialStore()
	account := "cache-key:" + resolved.Server
	encoded, err := store.Get(account)
	if err == nil {
		key, decodeErr := base64.RawStdEncoding.DecodeString(encoded)
		if decodeErr == nil && len(key) == 32 {
			return key, nil
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := store.Set(account, base64.RawStdEncoding.EncodeToString(key)); err != nil {
		return nil, err
	}
	return key, nil
}

func cachePath(resolved *resolvedContext) (string, error) {
	dir := os.Getenv("CLAVIS_CACHE_DIR")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "clavis", "cache")
	}
	hash := sha256.Sum256([]byte(cacheScope(resolved)))
	return filepath.Join(dir, hex.EncodeToString(hash[:])[:32]+".json"), nil
}

func saveSecretCache(resolved *resolvedContext, secrets []secretJSON) error {
	key, err := cacheKey(resolved)
	if err != nil {
		return err
	}
	path, err := cachePath(resolved)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(cachedSecrets{CreatedAt: time.Now().UTC(), Secrets: secrets})
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nonce, nonce, payload, []byte(cacheScope(resolved)))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, ciphertext, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadSecretCache(resolved *resolvedContext) ([]secretJSON, error) {
	key, err := cacheKey(resolved)
	if err != nil {
		return nil, err
	}
	path, err := cachePath(resolved)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("cache: truncated")
	}
	payload, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(cacheScope(resolved)))
	if err != nil {
		return nil, fmt.Errorf("cache: decrypt: %w", err)
	}
	var cached cachedSecrets
	if err := json.Unmarshal(payload, &cached); err != nil {
		return nil, err
	}
	if time.Since(cached.CreatedAt) > cacheTTL {
		return nil, errors.New("cache: expired")
	}
	return cached.Secrets, nil
}
