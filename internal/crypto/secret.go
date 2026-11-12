// Package crypto contains Clavis' envelope encryption, key hierarchy, and
// blind-index primitives.
//
// Nothing in this package may ever log or serialize plaintext secrets. The
// Secret type below enforces that at the formatting layer; callers must still
// avoid writing raw byte slices.
package crypto

import (
	"encoding/json"
	"log/slog"
)

// redacted is the only representation of a secret that may reach logs, error
// messages, or JSON responses.
const redacted = "[REDACTED]"

// Secret is a byte slice that refuses to reveal itself through common
// formatting paths. Use Bytes when the plaintext is genuinely needed (for
// example to hand it to AES-GCM), and always call Zero when finished.
type Secret []byte

// NewSecret copies b into a Secret so the caller's buffer can be wiped
// independently.
func NewSecret(b []byte) Secret {
	s := make(Secret, len(b))
	copy(s, b)
	return s
}

// Bytes returns the underlying bytes. The caller must not retain them beyond
// the immediate operation.
func (s Secret) Bytes() []byte { return []byte(s) }

// Len reports the plaintext length.
func (s Secret) Len() int { return len(s) }

// Zero overwrites the plaintext in place, best effort.
func (s Secret) Zero() {
	for i := range s {
		s[i] = 0
	}
}

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// String implements fmt.Stringer.
func (s Secret) String() string { return redacted }

// GoString implements fmt.GoStringer for %#v.
func (s Secret) GoString() string { return redacted }

// MarshalJSON ensures secrets cannot be accidentally returned by an API
// handler that serializes a struct containing one.
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

// MarshalText ensures secrets cannot leak through text encoders.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
