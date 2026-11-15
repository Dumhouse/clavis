package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// errRootKeyMismatch means the root key cannot open this database's org keys.
var errRootKeyMismatch = errors.New("root key does not match this database")

// guardMissingRootKeyFile stops the file provider from generating a new root key
// for a database that already holds data. A fresh key cannot decrypt anything
// stored under the old one, so starting would look healthy while every secret
// was lost. This happens when root.key is not restored alongside the database.
func guardMissingRootKeyFile(ctx context.Context, cfg *config.Config, st *store.Store) error {
	if cfg.Keys.Provider != "file" {
		return nil
	}
	if _, err := os.Stat(cfg.Keys.RootKeyFile); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	_, _, hasOrgs, err := st.OldestOrgKey(ctx)
	if err != nil {
		return err
	}
	if !hasOrgs {
		return nil
	}
	return fmt.Errorf("server: root key %s is missing but the database already has data; "+
		"restore the root key file from your backup (a new key cannot decrypt existing secrets)",
		cfg.Keys.RootKeyFile)
}

// verifyRootKey checks that the loaded root key unwraps the oldest org key, so
// a wrong key (a mistyped CLAVIS_ROOT_KEY, a key from another install) fails at
// startup instead of on the first secret read.
func verifyRootKey(ctx context.Context, st *store.Store, key crypto.Key) error {
	orgID, wrapped, ok, err := st.OldestOrgKey(ctx)
	if err != nil || !ok {
		return err
	}
	orgKey, err := crypto.NewKeyring(key).UnwrapOrgKey(orgID, wrapped)
	if err != nil {
		return fmt.Errorf("server: %w (check the root key provider and its value): %v", errRootKeyMismatch, err)
	}
	orgKey.Zero()
	return nil
}
