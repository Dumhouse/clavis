package cli

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestBackupEncryptionRoundTripAndTamper(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("database bytes with wrapped keys")
	encrypted, err := encryptBackup(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := decryptBackup(key, encrypted)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypt = %q %v", decrypted, err)
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := decryptBackup(key, encrypted); err == nil {
		t.Fatal("tampered backup decrypted")
	}
}
