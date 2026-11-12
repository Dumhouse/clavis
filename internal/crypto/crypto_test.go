package crypto

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(t *testing.T) Key {
	t.Helper()
	k, err := NewKeyFromBytes(bytes.Repeat([]byte{0x42}, KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	aad := BindAAD("org", "proj", "env", "item", "value", 3)
	plaintext := NewSecret([]byte("sk_live_super_secret"))

	ct, err := key.Encrypt(7, plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, version, err := key.Decrypt(ct, aad)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Zero()
	if version != 7 {
		t.Fatalf("key version = %d, want 7", version)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext = %q, want %q", got, plaintext)
	}
}

func TestDecryptRejectsSwappedCiphertext(t *testing.T) {
	key := testKey(t)
	ct, err := key.Encrypt(1, NewSecret([]byte("value")), BindAAD("org", "proj", "env", "item-a", "value", 1))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = key.Decrypt(ct, BindAAD("org", "proj", "env", "item-b", "value", 1))
	if !errors.Is(err, ErrDecrypt) {
		t.Fatalf("err = %v, want ErrDecrypt", err)
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	key := testKey(t)
	aad := BindAAD("o", "p", "e", "i", "value", 1)
	ct, err := key.Encrypt(1, NewSecret([]byte("value")), aad)
	if err != nil {
		t.Fatal(err)
	}
	ct[len(ct)-1] ^= 0xff
	if _, _, err := key.Decrypt(ct, aad); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("err = %v, want ErrDecrypt", err)
	}
}

func TestDecryptRejectsMalformed(t *testing.T) {
	key := testKey(t)
	aad := []byte("aad")
	if _, _, err := key.Decrypt(nil, aad); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("short ciphertext: err = %v", err)
	}
	ct, _ := key.Encrypt(1, NewSecret([]byte("x")), aad)
	ct[0] = 99
	if _, _, err := key.Decrypt(ct, aad); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("bad version: err = %v", err)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	k1 := testKey(t)
	k2, _ := GenerateKey()
	aad := []byte("aad")
	ct, _ := k1.Encrypt(1, NewSecret([]byte("x")), aad)
	if _, _, err := k2.Decrypt(ct, aad); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("err = %v, want ErrDecrypt", err)
	}
}

func TestKeyValidation(t *testing.T) {
	if _, err := NewKeyFromBytes([]byte("too short")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("err = %v, want ErrInvalidKey", err)
	}
}

func FuzzDecrypt(f *testing.F) {
	key, _ := NewKeyFromBytes(bytes.Repeat([]byte{0x42}, KeySize))
	aad := []byte("aad")
	ct, _ := key.Encrypt(1, NewSecret([]byte("seed")), aad)
	f.Add(ct)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		got, _, err := key.Decrypt(data, aad)
		if err == nil {
			got.Zero()
		}
	})
}

func TestSecretRedaction(t *testing.T) {
	s := NewSecret([]byte("hunter2"))
	if s.String() != redacted || s.GoString() != redacted {
		t.Fatal("secret formatted as plaintext")
	}
	if got := s.LogValue().String(); got != redacted {
		t.Fatalf("log value = %q", got)
	}
	b, err := json.Marshal(struct{ Value Secret }{Value: s})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hunter2") {
		t.Fatalf("JSON leaked secret: %s", b)
	}
	b, _ = s.MarshalText()
	if string(b) != redacted {
		t.Fatalf("text = %q", b)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("test", "secret", s)
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("log leaked secret: %s", buf.String())
	}
}

func TestSecretZero(t *testing.T) {
	s := NewSecret([]byte("abc"))
	s.Zero()
	if !bytes.Equal(s, []byte{0, 0, 0}) {
		t.Fatalf("not zeroed: %v", s)
	}
	if s.Len() != 3 || !bytes.Equal(s.Bytes(), []byte{0, 0, 0}) {
		t.Fatal("unexpected secret accessors")
	}
}

func TestKeyHierarchy(t *testing.T) {
	root, _ := GenerateKey()
	kr := NewKeyring(root)

	wrappedOrg, orgKey, err := kr.NewOrgKey("org-1")
	if err != nil {
		t.Fatal(err)
	}
	gotOrg, err := kr.UnwrapOrgKey("org-1", wrappedOrg)
	if err != nil {
		t.Fatal(err)
	}
	if gotOrg != orgKey {
		t.Fatal("org key mismatch")
	}
	if _, err := kr.UnwrapOrgKey("org-2", wrappedOrg); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("org binding not enforced: %v", err)
	}

	projectKey, _ := GenerateKey()
	wrappedProject, err := WrapProjectKey(gotOrg, "org-1", "proj-1", projectKey)
	if err != nil {
		t.Fatal(err)
	}
	gotProject, err := UnwrapProjectKey(gotOrg, "org-1", "proj-1", wrappedProject)
	if err != nil {
		t.Fatal(err)
	}
	if gotProject != projectKey {
		t.Fatal("project key mismatch")
	}
	if _, err := UnwrapProjectKey(gotOrg, "org-1", "proj-2", wrappedProject); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("project binding not enforced: %v", err)
	}
}

func TestDerivedKeys(t *testing.T) {
	projectKey := testKey(t)
	valueKey, err := DeriveValueKey(projectKey)
	if err != nil {
		t.Fatal(err)
	}
	indexKey, err := DeriveBlindIndexKey(projectKey)
	if err != nil {
		t.Fatal(err)
	}
	if valueKey == indexKey {
		t.Fatal("derived keys must differ")
	}
	if valueKey == projectKey {
		t.Fatal("derived key must differ from project key")
	}

	a := BlindIndex(indexKey, "STRIPE_KEY")
	b := BlindIndex(indexKey, "STRIPE_KEY")
	c := BlindIndex(indexKey, "OTHER_KEY")
	if a != b {
		t.Fatal("blind index is not deterministic")
	}
	if a == c {
		t.Fatal("blind index collision")
	}
	if strings.Contains(a, "STRIPE") {
		t.Fatal("blind index leaks the key name")
	}
}

func TestBindAADIsUnambiguous(t *testing.T) {
	a := BindAAD("a", "bc", "", "", "", 1)
	b := BindAAD("ab", "c", "", "", "", 1)
	if bytes.Equal(a, b) {
		t.Fatal("length prefixing failed")
	}
}

func TestFileProviderGenerateAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "root.key")
	p := NewFileProvider(path, true)
	if p.Name() != "file" {
		t.Fatalf("name = %q", p.Name())
	}
	k1, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}

	k2, err := NewFileProvider(path, false).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Fatal("reload returned a different key")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileProviderMissing(t *testing.T) {
	p := NewFileProvider(filepath.Join(t.TempDir(), "missing.key"), false)
	if _, err := p.Load(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestProvidersHonorContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewFileProvider("x", true).Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("file: %v", err)
	}
	if _, err := NewEnvProvider("X").Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("env: %v", err)
	}
}

func TestFileProviderRejectsInsecurePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(path, []byte(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, KeySize))), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileProvider(path, false).Load(context.Background()); err == nil {
		t.Fatal("expected permission error")
	}
}

func TestKeyZero(t *testing.T) {
	key := testKey(t)
	key.Zero()
	if key != (Key{}) {
		t.Fatal("key not zeroed")
	}
}

func TestEnvProviderNameAndClose(t *testing.T) {
	p := NewEnvProvider("X")
	if p.Name() != "env" {
		t.Fatalf("name = %q", p.Name())
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEnvProvider(t *testing.T) {
	key, _ := GenerateKey()
	t.Setenv("CLAVIS_TEST_ROOT_KEY", base64.RawURLEncoding.EncodeToString(key[:]))
	got, err := NewEnvProvider("CLAVIS_TEST_ROOT_KEY").Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != key {
		t.Fatal("key mismatch")
	}
	if _, err := NewEnvProvider("CLAVIS_TEST_MISSING").Load(context.Background()); err == nil {
		t.Fatal("expected missing error")
	}
	t.Setenv("CLAVIS_TEST_BAD_ROOT_KEY", "not base64!")
	if _, err := NewEnvProvider("CLAVIS_TEST_BAD_ROOT_KEY").Load(context.Background()); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDecodeKeyVariants(t *testing.T) {
	raw := bytes.Repeat([]byte{7}, KeySize)
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if _, err := decodeKey(enc.EncodeToString(raw)); err != nil {
			t.Fatalf("%v: %v", enc, err)
		}
	}
	if _, err := decodeKey(fmt.Sprintf("%d", 42)); err == nil {
		t.Fatal("expected error")
	}
}
