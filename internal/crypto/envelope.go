package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// KeySize is the size in bytes of every symmetric key in Clavis.
	KeySize = 32
	// NonceSize is the AES-GCM nonce size.
	NonceSize = 12
	// TagSize is the AES-GCM authentication tag size.
	TagSize = 16
	// EnvelopeVersion is the current ciphertext format version.
	EnvelopeVersion byte = 1

	headerSize = 1 + 4 + NonceSize // version | key version | nonce
	minSize    = headerSize + TagSize
)

var (
	// ErrInvalidKey is returned when a key is not 32 bytes.
	ErrInvalidKey = errors.New("crypto: key must be 32 bytes")
	// ErrInvalidCiphertext is returned for malformed or truncated ciphertexts.
	ErrInvalidCiphertext = errors.New("crypto: invalid ciphertext")
	// ErrPlaintextTooLarge is returned when a value exceeds MaxPlaintextSize.
	ErrPlaintextTooLarge = errors.New("crypto: plaintext too large")
	// ErrUnknownVersion is returned for a ciphertext produced by a newer,
	// unsupported format version.
	ErrUnknownVersion = errors.New("crypto: unknown envelope version")
	// ErrDecrypt is returned when authentication fails. It intentionally does
	// not distinguish swapped ciphertexts, wrong keys, or tampering.
	ErrDecrypt = errors.New("crypto: decryption failed")
)

// Key is a 256-bit symmetric key.
type Key [KeySize]byte

// NewKeyFromBytes validates and copies raw key material.
func NewKeyFromBytes(b []byte) (Key, error) {
	var k Key
	if len(b) != KeySize {
		return k, ErrInvalidKey
	}
	copy(k[:], b)
	return k, nil
}

// GenerateKey returns a cryptographically random key.
func GenerateKey() (Key, error) {
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		return k, fmt.Errorf("crypto: generate key: %w", err)
	}
	return k, nil
}

// Zero overwrites the key in place.
func (k *Key) Zero() {
	for i := range k {
		k[i] = 0
	}
}

// MaxPlaintextSize bounds the value Encrypt accepts (1 GiB), far above any
// secret, so the output buffer size cannot overflow.
const MaxPlaintextSize = 1 << 30

// Encrypt seals plaintext with AES-256-GCM. The returned ciphertext uses the
// versioned envelope format:
//
//	[1 byte version][4 byte big-endian key version][12 byte nonce][ciphertext+tag]
//
// aad binds the ciphertext to its logical location (org/project/env/item/field/
// version). Decryption with different aad fails.
func (k Key) Encrypt(keyVersion uint32, plaintext Secret, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}

	if len(plaintext) > MaxPlaintextSize {
		return nil, ErrPlaintextTooLarge
	}
	out := make([]byte, headerSize, headerSize+len(plaintext)+TagSize)
	out[0] = EnvelopeVersion
	binary.BigEndian.PutUint32(out[1:5], keyVersion)
	if _, err := rand.Read(out[5:headerSize]); err != nil {
		return nil, fmt.Errorf("crypto: nonce: %w", err)
	}

	nonce := out[5:headerSize]
	out = gcm.Seal(out, nonce, plaintext, aad)
	return out, nil
}

// Decrypt opens a ciphertext produced by Encrypt.
func (k Key) Decrypt(ciphertext, aad []byte) (Secret, uint32, error) {
	if len(ciphertext) < minSize {
		return nil, 0, ErrInvalidCiphertext
	}
	if ciphertext[0] != EnvelopeVersion {
		return nil, 0, ErrUnknownVersion
	}
	keyVersion := binary.BigEndian.Uint32(ciphertext[1:5])
	nonce := ciphertext[5:headerSize]
	body := ciphertext[headerSize:]

	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, 0, ErrDecrypt
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, 0, ErrDecrypt
	}
	plaintext, err := gcm.Open(nil, nonce, body, aad)
	if err != nil {
		return nil, 0, ErrDecrypt
	}
	return Secret(plaintext), keyVersion, nil
}

// BindAAD constructs associated data for a ciphertext. Every field is length
// prefixed so that values containing separators cannot collide.
func BindAAD(orgID, projectID, envID, itemID, field string, version int) []byte {
	fields := [...]string{orgID, projectID, envID, itemID, field, fmt.Sprintf("%d", version)}
	size := 0
	for _, f := range fields {
		size += 2 + len(f)
	}
	out := make([]byte, 0, size)
	var l [2]byte
	for _, f := range fields {
		binary.BigEndian.PutUint16(l[:], uint16(len(f))) //nolint:gosec // G115: AAD fields are IDs and short labels, far below 64 KiB
		out = append(out, l[:]...)
		out = append(out, f...)
	}
	return out
}
