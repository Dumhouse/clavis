package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	totpSkew   = 1
	// RecoveryCodeCount is how many one-time codes enrollment returns.
	RecoveryCodeCount = 10
)

var recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateTOTPSecret returns 160 bits of random key material.
func GenerateTOTPSecret() ([]byte, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("authn: totp secret: %w", err)
	}
	return secret, nil
}

// TOTPCode computes the 6-digit code for a time.
func TOTPCode(secret []byte, at time.Time) string {
	counter := uint64(at.Unix() / int64(totpPeriod/time.Second))
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}

// ValidateTOTP checks a code at time at, allowing one step of clock skew.
func ValidateTOTP(secret []byte, code string, at time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	for offset := -totpSkew; offset <= totpSkew; offset++ {
		candidate := TOTPCode(secret, at.Add(time.Duration(offset)*totpPeriod))
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPURL returns an otpauth:// URI for authenticator apps.
func TOTPURL(issuer, account string, secret []byte) string {
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	label := url.PathEscape(issuer + ":" + account)
	values := url.Values{}
	values.Set("secret", encoded)
	values.Set("issuer", issuer)
	values.Set("algorithm", "SHA1")
	values.Set("digits", fmt.Sprintf("%d", totpDigits))
	values.Set("period", fmt.Sprintf("%d", int(totpPeriod/time.Second)))
	return "otpauth://totp/" + label + "?" + values.Encode()
}

// GenerateRecoveryCodes returns plaintext codes and their stored hashes.
func GenerateRecoveryCodes() ([]string, [][]byte, error) {
	codes := make([]string, 0, RecoveryCodeCount)
	hashes := make([][]byte, 0, RecoveryCodeCount)
	for i := 0; i < RecoveryCodeCount; i++ {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("authn: recovery code: %w", err)
		}
		var b strings.Builder
		for j := range raw {
			if j == 5 {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryAlphabet[int(raw[j])%len(recoveryAlphabet)])
		}
		code := b.String()
		codes = append(codes, code)
		hashes = append(hashes, HashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// HashRecoveryCode normalizes and hashes a recovery code for storage/lookup.
func HashRecoveryCode(code string) []byte {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	sum := sha256.Sum256([]byte(normalized))
	return sum[:]
}
