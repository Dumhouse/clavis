// Package authn implements passwords, setup tokens, sessions, CSRF, and
// browser login for Clavis.
package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password policy: minimum length only, no composition rules (NIST 800-63B).
const (
	MinPasswordLength = 12
	MaxPasswordLength = 1024

	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

var (
	// ErrInvalidPassword is returned for a password that fails the policy.
	ErrInvalidPassword = errors.New("authn: password must be between 12 and 1024 characters")
	// ErrInvalidHash is returned for a malformed stored password hash.
	ErrInvalidHash = errors.New("authn: invalid password hash")
)

// ValidatePassword enforces the password policy.
func ValidatePassword(password string) error {
	// Count runes so non-ASCII passphrases are treated fairly.
	n := len([]rune(password))
	if n < MinPasswordLength || len(password) > MaxPasswordLength {
		return ErrInvalidPassword
	}
	return nil
}

// HashPassword returns an argon2id PHC string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("authn: salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword checks password against a PHC-encoded argon2id hash in
// constant time. It returns false (not an error) for a wrong password.
func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, want, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want))) //nolint:gosec // G115: parseHash bounds the hash length to 16..128 bytes
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parseHash(encoded string) (argonParams, []byte, []byte, error) {
	var zero argonParams
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return zero, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return zero, nil, nil, ErrInvalidHash
	}

	var params argonParams
	var m, t, p int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return zero, nil, nil, ErrInvalidHash
	}
	if m < 8*1024 || m > 1024*1024 || t < 1 || t > 32 || p < 1 || p > 64 {
		return zero, nil, nil, ErrInvalidHash
	}
	params.memory = uint32(m)
	params.time = uint32(t)
	params.threads = uint8(p)

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return zero, nil, nil, ErrInvalidHash
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) < 16 || len(hash) > 128 {
		return zero, nil, nil, ErrInvalidHash
	}
	return params, salt, hash, nil
}

// ParseParams exposes memory/time/threads for tests and future rehash-on-login.
func ParseParams(encoded string) (memory, iter uint32, threads uint8, err error) {
	p, _, _, err := parseHash(encoded)
	if err != nil {
		return 0, 0, 0, err
	}
	return p.memory, p.time, p.threads, nil
}
