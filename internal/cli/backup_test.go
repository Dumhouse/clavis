package cli

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/dumhouse/clavis/internal/backup"
)

func TestBackupEncryptionRoundTripAndTamper(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("database bytes with wrapped keys")
	encrypted, err := backup.Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := backup.Decrypt(key, encrypted)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypt = %q %v", decrypted, err)
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := backup.Decrypt(key, encrypted); err == nil {
		t.Fatal("tampered backup decrypted")
	}
}
