package crypto

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// HKDF info strings. Changing these is a breaking crypto migration.
const (
	infoSecretValue  = "clavis/secret-value/v1"
	infoBlindIndex   = "clavis/blind-index/v1"
	infoMFA          = "clavis/mfa/v1"
	infoAuditSigning = "clavis/audit-signing/v1"
)

// Keyring owns the root key and performs wrapping/unwrapping in the key
// hierarchy:
//
//	root -> org -> project -> HKDF(value key, blind-index key)
type Keyring struct {
	root Key
}

// NewKeyring returns a keyring rooted at root.
func NewKeyring(root Key) *Keyring { return &Keyring{root: root} }

// NewOrgKey generates an org key and wraps it with the root key.
func (kr *Keyring) NewOrgKey(orgID string) (wrapped []byte, orgKey Key, err error) {
	orgKey, err = GenerateKey()
	if err != nil {
		return nil, Key{}, err
	}
	wrapped, err = kr.wrap(kr.root, orgKey, wrapAAD("org", orgID, ""))
	if err != nil {
		orgKey.Zero()
		return nil, Key{}, err
	}
	return wrapped, orgKey, nil
}

// UnwrapOrgKey unwraps an org key previously wrapped by NewOrgKey.
func (kr *Keyring) UnwrapOrgKey(orgID string, wrapped []byte) (Key, error) {
	return kr.unwrap(kr.root, wrapped, wrapAAD("org", orgID, ""))
}

// NewSharedSetKey generates a shared-set key and wraps it with the org key.
func (kr *Keyring) NewSharedSetKey(orgID, setID string) (wrapped []byte, key Key, err error) {
	key, err = GenerateKey()
	if err != nil {
		return nil, Key{}, err
	}
	wrapped, err = kr.wrap(kr.root, key, wrapAAD("shared_set", orgID, setID))
	if err != nil {
		key.Zero()
		return nil, Key{}, err
	}
	return wrapped, key, nil
}

// UnwrapSharedSetKey unwraps a shared-set key.
func (kr *Keyring) UnwrapSharedSetKey(orgID, setID string, wrapped []byte) (Key, error) {
	return kr.unwrap(kr.root, wrapped, wrapAAD("shared_set", orgID, setID))
}

// RewrapOrgKey unwraps an org key with this keyring and wraps it with newRoot.
func (kr *Keyring) RewrapOrgKey(orgID string, wrapped []byte, newRoot Key) ([]byte, error) {
	orgKey, err := kr.UnwrapOrgKey(orgID, wrapped)
	if err != nil {
		return nil, err
	}
	defer orgKey.Zero()
	next := NewKeyring(newRoot)
	return next.wrap(newRoot, orgKey, wrapAAD("org", orgID, ""))
}

// WrapProjectKey wraps a project key with its parent org key.
func WrapProjectKey(orgKey Key, orgID, projectID string, projectKey Key) ([]byte, error) {
	return (&Keyring{root: orgKey}).wrap(orgKey, projectKey, wrapAAD("project", orgID, projectID))
}

// UnwrapProjectKey unwraps a project key using its parent org key.
func UnwrapProjectKey(orgKey Key, orgID, projectID string, wrapped []byte) (Key, error) {
	return (&Keyring{root: orgKey}).unwrap(orgKey, wrapped, wrapAAD("project", orgID, projectID))
}

func (kr *Keyring) wrap(kek, key Key, aad []byte) ([]byte, error) {
	return kek.Encrypt(1, Secret(key[:]), aad)
}

func (kr *Keyring) unwrap(kek Key, wrapped, aad []byte) (Key, error) {
	plaintext, _, err := kek.Decrypt(wrapped, aad)
	if err != nil {
		return Key{}, err
	}
	defer plaintext.Zero()
	key, err := NewKeyFromBytes(plaintext)
	if err != nil {
		return Key{}, err
	}
	return key, nil
}

func wrapAAD(kind, parentID, childID string) []byte {
	return []byte(fmt.Sprintf("clavis/wrap/v1|%s|%s|%s", kind, parentID, childID))
}

// DeriveValueKey derives the key that encrypts secret values for a project.
func DeriveValueKey(projectKey Key) (Key, error) {
	return derive(projectKey, infoSecretValue)
}

// DeriveAuditSigningKey derives the Ed25519 key used for audit checkpoints.
func DeriveAuditSigningKey(root Key) (ed25519.PrivateKey, error) {
	seed, err := derive(root, infoAuditSigning)
	if err != nil {
		return nil, err
	}
	defer seed.Zero()
	return ed25519.NewKeyFromSeed(seed[:]), nil
}

// DeriveMFAKey derives the key that encrypts TOTP secrets at rest.
func DeriveMFAKey(root Key) (Key, error) {
	return derive(root, infoMFA)
}

// DeriveBlindIndexKey derives the HMAC key used for encrypted key-name lookup.
func DeriveBlindIndexKey(projectKey Key) (Key, error) {
	return derive(projectKey, infoBlindIndex)
}

func derive(key Key, info string) (Key, error) {
	var out Key
	r := hkdf.New(sha256.New, key[:], nil, []byte(info))
	if _, err := io.ReadFull(r, out[:]); err != nil {
		return Key{}, fmt.Errorf("crypto: hkdf: %w", err)
	}
	return out, nil
}

// BlindIndex computes a deterministic, non-reversible lookup token for a secret
// key name. It is case-sensitive because environment variable names are.
func BlindIndex(indexKey Key, name string) string {
	m := hmac.New(sha256.New, indexKey[:])
	m.Write([]byte(name))
	return hex.EncodeToString(m.Sum(nil))
}
