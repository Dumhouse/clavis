package audit

import (
	"crypto/ed25519"
	"fmt"
	"time"
)

// Checkpoint is a signed statement that an org's audit chain had a given hash at
// a given sequence number.
type Checkpoint struct {
	ID        string
	OrgID     string
	Seq       int64
	EventHash string
	Signature []byte
	PublicKey []byte
	CreatedAt time.Time
}

// CheckpointMessage is the canonical signed payload.
func CheckpointMessage(orgID string, seq int64, eventHash string, at time.Time) []byte {
	return []byte(fmt.Sprintf("clavis/checkpoint/v1\x00%s\x00%d\x00%s\x00%s",
		orgID, seq, eventHash, at.UTC().Format(time.RFC3339Nano)))
}

// SignCheckpoint signs a checkpoint in place.
func SignCheckpoint(private ed25519.PrivateKey, checkpoint *Checkpoint) error {
	if len(private) != ed25519.PrivateKeySize {
		return fmt.Errorf("audit: invalid signing key")
	}
	if checkpoint.Seq <= 0 || checkpoint.EventHash == "" || checkpoint.CreatedAt.IsZero() {
		return fmt.Errorf("audit: incomplete checkpoint")
	}
	checkpoint.PublicKey = append([]byte(nil), private.Public().(ed25519.PublicKey)...)
	checkpoint.Signature = ed25519.Sign(private,
		CheckpointMessage(checkpoint.OrgID, checkpoint.Seq, checkpoint.EventHash, checkpoint.CreatedAt))
	return nil
}

// VerifyCheckpoint checks the checkpoint signature against the public key it
// carries.
func VerifyCheckpoint(checkpoint *Checkpoint) error {
	if len(checkpoint.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("audit: invalid checkpoint public key")
	}
	if len(checkpoint.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("audit: invalid checkpoint signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(checkpoint.PublicKey),
		CheckpointMessage(checkpoint.OrgID, checkpoint.Seq, checkpoint.EventHash, checkpoint.CreatedAt),
		checkpoint.Signature) {
		return fmt.Errorf("audit: checkpoint signature did not verify")
	}
	return nil
}
