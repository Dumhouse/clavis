package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

func guardStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "clavis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func addOrgUnder(t *testing.T, st *store.Store, root crypto.Key) {
	t.Helper()
	org := &store.Org{ID: "org-1", Slug: "acme", Name: "Acme"}
	wrapped, orgKey, err := crypto.NewKeyring(root).NewOrgKey(org.ID)
	if err != nil {
		t.Fatal(err)
	}
	orgKey.Zero()
	org.WrappedOrgKey = wrapped
	if err := st.CreateOrg(context.Background(), org); err != nil {
		t.Fatal(err)
	}
}

func TestGuardMissingRootKeyFile(t *testing.T) {
	ctx := context.Background()
	st, dir := guardStore(t)
	cfg := &config.Config{}
	cfg.Keys.Provider = "file"
	cfg.Keys.RootKeyFile = filepath.Join(dir, "root.key")

	if err := guardMissingRootKeyFile(ctx, cfg, st); err != nil {
		t.Fatalf("empty database must allow a new key: %v", err)
	}

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	addOrgUnder(t, st, key)
	err = guardMissingRootKeyFile(ctx, cfg, st)
	if err == nil || !strings.Contains(err.Error(), "restore the root key file") {
		t.Fatalf("missing key with data: got %v", err)
	}

	cfg.Keys.Provider = "env"
	if err := guardMissingRootKeyFile(ctx, cfg, st); err != nil {
		t.Fatalf("env provider is not guarded here: %v", err)
	}
}

func TestVerifyRootKey(t *testing.T) {
	ctx := context.Background()
	st, _ := guardStore(t)
	right, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	if err := verifyRootKey(ctx, st, wrong); err != nil {
		t.Fatalf("any key fits an empty database: %v", err)
	}
	addOrgUnder(t, st, right)
	if err := verifyRootKey(ctx, st, right); err != nil {
		t.Fatalf("right key rejected: %v", err)
	}
	if err := verifyRootKey(ctx, st, wrong); !errors.Is(err, errRootKeyMismatch) {
		t.Fatalf("wrong key: got %v", err)
	}
}
