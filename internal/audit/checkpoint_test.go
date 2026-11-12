package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestCheckpointSignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := &Checkpoint{
		OrgID: "org-1", Seq: 7, EventHash: "abc123", CreatedAt: time.Now().UTC(),
	}
	if err := SignCheckpoint(priv, checkpoint); err != nil {
		t.Fatal(err)
	}
	if string(checkpoint.PublicKey) != string(pub) {
		t.Fatal("public key not stored")
	}
	if err := VerifyCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}

	checkpoint.EventHash = "tampered"
	if err := VerifyCheckpoint(checkpoint); err == nil {
		t.Fatal("tampered checkpoint verified")
	}
	checkpoint.EventHash = "abc123"
	checkpoint.Seq = 8
	if err := VerifyCheckpoint(checkpoint); err == nil {
		t.Fatal("changed sequence verified")
	}
	if err := SignCheckpoint(ed25519.PrivateKey("short"), checkpoint); err == nil {
		t.Fatal("short key accepted")
	}
	if err := VerifyCheckpoint(&Checkpoint{}); err == nil {
		t.Fatal("empty checkpoint verified")
	}
}
