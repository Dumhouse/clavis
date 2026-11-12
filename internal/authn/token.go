package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash/crc32"
	"math/big"
	"strings"
)

// Token kinds. The prefix is visible in logs and enables secret scanning.
const (
	TokenMachine  = "mt"
	TokenAgent    = "at"
	TokenPersonal = "pat"
)

var (
	// ErrInvalidTokenFormat means the prefix or checksum is wrong.
	ErrInvalidTokenFormat = errors.New("authn: invalid token format")
	tokenAlphabet         = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// GenerateToken returns clv_<kind>_<base62 random>+<crc32>.
func GenerateToken(kind string) (string, error) {
	switch kind {
	case TokenMachine, TokenAgent, TokenPersonal:
	default:
		return "", fmt.Errorf("authn: unknown token kind %q", kind)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("authn: token random: %w", err)
	}
	body := base62Encode(raw)
	check := crc32.ChecksumIEEE([]byte(kind + "_" + body))
	return "clv_" + kind + "_" + body + fmt.Sprintf("%08x", check), nil
}

// ParseToken validates a token's structure and checksum and returns its kind.
func ParseToken(token string) (string, error) {
	if len(token) < 20 || len(token) > 128 {
		return "", ErrInvalidTokenFormat
	}
	parts := strings.SplitN(token, "_", 3)
	if len(parts) != 3 || parts[0] != "clv" {
		return "", ErrInvalidTokenFormat
	}
	kind, rest := parts[1], parts[2]
	switch kind {
	case TokenMachine, TokenAgent, TokenPersonal:
	default:
		return "", ErrInvalidTokenFormat
	}
	if len(rest) < 9 {
		return "", ErrInvalidTokenFormat
	}
	body := rest[:len(rest)-8]
	if body == "" {
		return "", ErrInvalidTokenFormat
	}
	for _, r := range body {
		if !strings.ContainsRune(tokenAlphabet, r) {
			return "", ErrInvalidTokenFormat
		}
	}
	var want uint32
	if _, err := fmt.Sscanf(rest[len(rest)-8:], "%08x", &want); err != nil {
		return "", ErrInvalidTokenFormat
	}
	if crc32.ChecksumIEEE([]byte(kind+"_"+body)) != want {
		return "", ErrInvalidTokenFormat
	}
	return kind, nil
}

// HashToken returns the SHA-256 used for storage and lookup.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// TokenPrefix returns a short, non-secret prefix for display.
func TokenPrefix(token string) string {
	if len(token) <= 16 {
		return token
	}
	return token[:16] + "…"
}

// IsClavisToken reports whether a bearer value looks like a Clavis token.
func IsClavisToken(value string) bool {
	return strings.HasPrefix(value, "clv_")
}

func base62Encode(data []byte) string {
	n := new(big.Int).SetBytes(data)
	base := big.NewInt(int64(len(tokenAlphabet)))
	zero := big.NewInt(0)
	var out []byte
	for n.Cmp(zero) > 0 {
		mod := new(big.Int)
		n.DivMod(n, base, mod)
		out = append(out, tokenAlphabet[mod.Int64()])
	}
	if len(out) == 0 {
		return string(tokenAlphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}
