package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestShamirSplitAndCombine(t *testing.T) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	shares, err := SplitSecret(secret, 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 5 {
		t.Fatalf("shares = %d", len(shares))
	}
	for _, subset := range [][][]byte{
		{shares[0], shares[1], shares[2]},
		{shares[0], shares[2], shares[4]},
		{shares[1], shares[3], shares[4]},
	} {
		recovered, err := CombineShares(subset, 3)
		if err != nil || !bytes.Equal(recovered, secret) {
			t.Fatalf("combine = %x %v", recovered, err)
		}
	}
	if _, err := CombineShares(shares[:2], 3); err == nil {
		t.Fatal("combined with too few shares")
	}
}
