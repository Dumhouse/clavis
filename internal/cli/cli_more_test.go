package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

func TestMaskWriterAcrossChunks(t *testing.T) {
	const secret = "super-secret-value"
	m := newMasker([]string{secret})
	var out bytes.Buffer
	w := &maskWriter{dst: &out, m: m}
	for _, chunk := range []string{"before ", "super-sec", "ret-value", " after"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("secret leaked: %q", out.String())
	}
	if !strings.Contains(out.String(), maskReplacement) {
		t.Fatalf("not masked: %q", out.String())
	}

	var encoded bytes.Buffer
	ew := &maskWriter{dst: &encoded, m: newMasker([]string{secret})}
	if _, err := ew.Write([]byte("value " + base64.StdEncoding.EncodeToString([]byte(secret)))); err != nil {
		t.Fatal(err)
	}
	_ = ew.Flush()
	if strings.Contains(encoded.String(), base64.StdEncoding.EncodeToString([]byte(secret))) {
		t.Fatalf("base64 leaked: %q", encoded.String())
	}

	var urlOut bytes.Buffer
	uw := &maskWriter{dst: &urlOut, m: newMasker([]string{"a value/with?chars"})}
	_, _ = uw.Write([]byte(url.QueryEscape("a value/with?chars")))
	_ = uw.Flush()
	if strings.Contains(urlOut.String(), url.QueryEscape("a value/with?chars")) {
		t.Fatalf("url-escaped leaked: %q", urlOut.String())
	}
}

func TestMaskerShortAndEmpty(t *testing.T) {
	m := newMasker([]string{"abc", ""})
	var out bytes.Buffer
	w := &maskWriter{dst: &out, m: m}
	_, _ = w.Write([]byte("abc"))
	_ = w.Flush()
	if out.String() != "abc" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestFileCredentialStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	t.Setenv("CLAVIS_CREDENTIAL_FILE", path)
	store := credentialStore()
	if _, err := store.Get("https://x"); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("expected no credential, got %v", err)
	}
	if err := store.Set("https://x", "tok"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	got, err := store.Get("https://x")
	if err != nil || got != "tok" {
		t.Fatalf("get = %q %v", got, err)
	}
	if err := store.Delete("https://x"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("https://x"); !errors.Is(err, ErrNoCredential) {
		t.Fatal("credential not deleted")
	}
}

func TestGenerateValue(t *testing.T) {
	pw, err := generateValue("password", 24)
	if err != nil || len(pw) != 24 {
		t.Fatalf("password = %q %v", pw, err)
	}
	hexValue, err := generateValue("hex", 10)
	if err != nil || len(hexValue) != 10 {
		t.Fatalf("hex = %q %v", hexValue, err)
	}
	b64, err := generateValue("base64", 16)
	if err != nil || b64 == "" {
		t.Fatalf("base64 = %q %v", b64, err)
	}
	id, err := generateValue("uuid", 0)
	if err != nil || len(id) != 36 {
		t.Fatalf("uuid = %q %v", id, err)
	}
	if _, err := generateValue("nope", 10); err == nil {
		t.Fatal("unknown type accepted")
	}
}

func TestEncodeExport(t *testing.T) {
	values := map[string]string{"A": "one", "B": `two "quoted"`}
	r := &resolvedContext{Project: "web", Env: "dev"}
	for _, format := range []string{"dotenv", "docker", "json", "yaml", "shell", "k8s"} {
		content, err := encodeExport(format, r, values)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if !strings.Contains(content, `"A"`) && !strings.Contains(content, "A=") && !strings.Contains(content, "A:") {
			t.Errorf("%s output missing key: %s", format, content)
		}
	}
	shell, _ := encodeExport("shell", r, map[string]string{"A": "it's"})
	if !strings.Contains(shell, `'it'\''s'`) {
		t.Fatalf("shell escaping = %q", shell)
	}
	dotenv, _ := encodeExport("dotenv", r, map[string]string{"A": "has space"})
	if !strings.Contains(dotenv, `A="has space"`) {
		t.Fatalf("dotenv quoting = %q", dotenv)
	}
	if _, err := encodeExport("nope", r, nil); err == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestResolveContextFromProjectFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := writeProjectConfig(dir, &projectConfig{Server: "https://clavis.example.com", Org: "acme", Project: "web", Env: "staging"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAVIS_TOKEN", "tok")
	opts := &Options{}
	resolved, err := opts.resolveContext(true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Server != "https://clavis.example.com" || resolved.Org != "acme" || resolved.Project != "web" || resolved.Env != "staging" || resolved.Token != "tok" {
		t.Fatalf("resolved = %+v", resolved)
	}

	empty := t.TempDir()
	t.Chdir(empty)
	if _, err := (&Options{}).resolveContext(true); err == nil {
		t.Fatal("missing scope accepted")
	}
}

func TestSecretPathEscaping(t *testing.T) {
	r := &resolvedContext{Org: "acme", Project: "web app", Env: "dev"}
	got := secretPath(r, "A/B C")
	if strings.Contains(got, "web app") || !strings.Contains(got, "web%20app") {
		t.Fatalf("path escaping = %q", got)
	}
}

func TestAccessSubjectMatching(t *testing.T) {
	members := []memberJSON{
		{UserID: "u1", Email: "ada@example.com", Name: "Ada"},
		{UserID: "u2", Email: "grace@example.com", Name: "Grace"},
	}
	if got := matchMember(members, "ADA@example.com"); got != "u1" {
		t.Fatalf("email match = %q", got)
	}
	if got := matchMember(members, "Grace"); got != "u2" {
		t.Fatalf("name match = %q", got)
	}
	if got := matchMember(members, "u1"); got != "u1" {
		t.Fatalf("id match = %q", got)
	}
	if got := matchMember(members, "nobody"); got != "" {
		t.Fatalf("unexpected match %q", got)
	}
	groups := []groupJSON{{ID: "g1", Name: "Backend"}}
	if got := matchGroup(groups, "backend"); got != "g1" {
		t.Fatalf("group match = %q", got)
	}
	if got := scopeString("", ""); got != "all projects/*" {
		t.Fatalf("scope = %q", got)
	}
}

func TestCollectImportEntriesFromFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backend", "db"), 0o755); err != nil { //nolint:gosec // G301: test fixture directory
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("ROOT_KEY=one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", ".env"), []byte("API_KEY=two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "db", "values.yaml"), []byte("PASSWORD: three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "notes.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := collectImportEntries(root, "secret")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, entry := range entries {
		found[entry.folder+":"+entry.key] = entry.value
	}
	if found[":ROOT_KEY"] != "one" {
		t.Fatalf("root entry missing: %+v", found)
	}
	if found["/backend:API_KEY"] != "two" {
		t.Fatalf("backend entry missing: %+v", found)
	}
	if found["/backend/db:PASSWORD"] != "three" {
		t.Fatalf("nested entry missing: %+v", found)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestPrepareSecretEnvMountsFiles(t *testing.T) {
	secrets := []secretJSON{
		{Key: "PLAIN", Kind: "secret", Value: "plain-value"},
		{Key: "CERT_PEM", Kind: "file", Value: "-----BEGIN CERTIFICATE-----\nabc\n"},
	}
	entries, values, cleanup, err := prepareSecretEnv(secrets, true)
	if err != nil {
		t.Fatal(err)
	}
	foundPath := ""
	for _, entry := range entries {
		if strings.HasPrefix(entry, "PLAIN=") {
			if entry != "PLAIN=plain-value" {
				t.Fatalf("plain entry = %q", entry)
			}
		}
		if strings.HasPrefix(entry, "CERT_PEM=") {
			foundPath = strings.TrimPrefix(entry, "CERT_PEM=")
		}
	}
	if foundPath == "" {
		t.Fatalf("file entry missing: %+v", entries)
	}
	body, err := os.ReadFile(foundPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "-----BEGIN CERTIFICATE-----\nabc\n" {
		t.Fatalf("content = %q", body)
	}
	info, err := os.Stat(foundPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(foundPath))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}
	if len(values) != 1 || values[0] != "plain-value" {
		t.Fatalf("masked values = %+v", values)
	}
	cleanup()
	if _, err := os.Stat(foundPath); !os.IsNotExist(err) {
		t.Fatalf("mount dir survived cleanup: %v", err)
	}

	entries, _, cleanup, err = prepareSecretEnv(secrets, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(entries) != 2 {
		t.Fatalf("unmounted entries = %+v", entries)
	}
}

func TestRotateRootKeyRewrapsOrgsAndMFA(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.key")
	oldRoot, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := crypto.NewFileProvider(rootPath, false).Save(ctx, oldRoot); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "clavis.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	orgID := uuid.NewString()
	wrapped, _, err := crypto.NewKeyring(oldRoot).NewOrgKey(orgID)
	if err != nil {
		t.Fatal(err)
	}
	ownerRole := &store.Role{ID: uuid.NewString(), Name: "Owner", Permissions: "[]", Builtin: true}
	user := &store.User{ID: uuid.NewString(), Email: "owner@example.com", Name: "Owner", EmailVerified: true}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateOrgWithOwner(ctx, &store.Org{
		ID: orgID, Slug: "acme", Name: "Acme", Settings: "{}", WrappedOrgKey: wrapped, KeyVersion: 1,
	}, []*store.Role{ownerRole}, user.ID, ownerRole.ID); err != nil {
		t.Fatal(err)
	}
	oldMFA, err := crypto.DeriveMFAKey(oldRoot)
	if err != nil {
		t.Fatal(err)
	}
	mfaCiphertext, err := oldMFA.Encrypt(1, crypto.Secret("totp-secret"), []byte("clavis/mfa/v1|"+user.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMFASecret(ctx, user.ID, mfaCiphertext, true); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Database.Path = dbPath
	cfg.Keys.Provider = "file"
	cfg.Keys.RootKeyFile = rootPath
	var out, errOut bytes.Buffer
	output := NewTestOutput(&out, &errOut, false, false)
	if err := rotateRootKey(ctx, output, cfg, rootPath); err != nil {
		t.Fatalf("rotate: %v (stderr %s)", err, errOut.String())
	}

	newRoot, err := crypto.NewFileProvider(rootPath, false).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if newRoot == oldRoot {
		t.Fatal("root key did not change")
	}
	org, err := st.GetOrgByID(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	orgKey, err := crypto.NewKeyring(newRoot).UnwrapOrgKey(orgID, org.WrappedOrgKey)
	if err != nil {
		t.Fatalf("new root cannot unwrap org key: %v", err)
	}
	orgKey.Zero()
	if _, err := crypto.NewKeyring(oldRoot).UnwrapOrgKey(orgID, org.WrappedOrgKey); err == nil {
		t.Fatal("old root still unwraps org key")
	}
	updated, err := st.GetUserByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	newMFA, err := crypto.DeriveMFAKey(newRoot)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, _, err := newMFA.Decrypt(updated.MFASecretCiphertext, []byte("clavis/mfa/v1|"+user.ID))
	if err != nil {
		t.Fatalf("new MFA key cannot decrypt secret: %v", err)
	}
	if string(plaintext) != "totp-secret" {
		t.Fatalf("MFA secret = %q", plaintext)
	}
	backups, _ := filepath.Glob(rootPath + ".bak-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
}

func TestBuildOIDCTrustPresets(t *testing.T) {
	trust, err := buildOIDCTrust(oidcTrustOptions{
		Provider: "github", Repo: "acme/repo", Branch: "main", Environment: "prod",
		Claims: []string{"actor=octocat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if trust["issuer"] != "https://token.actions.githubusercontent.com" || trust["audience"] != "clavis" {
		t.Fatalf("github trust = %+v", trust)
	}
	claims, _ := trust["claims"].(map[string]string)
	if claims["repository"] != "acme/repo" || claims["ref"] != "refs/heads/main" || claims["environment"] != "prod" || claims["actor"] != "octocat" {
		t.Fatalf("github claims = %+v", claims)
	}

	trust, err = buildOIDCTrust(oidcTrustOptions{Provider: "gitlab", Repo: "group/project", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if trust["issuer"] != "https://gitlab.com" {
		t.Fatalf("gitlab trust = %+v", trust)
	}
	claims, _ = trust["claims"].(map[string]string)
	if claims["project_path"] != "group/project" || claims["ref"] != "refs/heads/main" {
		t.Fatalf("gitlab claims = %+v", claims)
	}

	trust, err = buildOIDCTrust(oidcTrustOptions{
		Provider: "kubernetes", Issuer: "https://kubernetes.default.svc.cluster.local",
		Audience: "clavis", K8sNamespace: "apps", K8sServiceAccount: "deployer",
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, _ = trust["claims"].(map[string]string)
	if claims["sub"] != "system:serviceaccount:apps:deployer" {
		t.Fatalf("k8s claims = %+v", claims)
	}

	trust, err = buildOIDCTrust(oidcTrustOptions{
		Provider: "gcp", GCPServiceAccount: "deployer@project.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if trust["issuer"] != "https://accounts.google.com" {
		t.Fatalf("gcp trust = %+v", trust)
	}
	claims, _ = trust["claims"].(map[string]string)
	if claims["email"] != "deployer@project.iam.gserviceaccount.com" {
		t.Fatalf("gcp claims = %+v", claims)
	}

	if _, err := buildOIDCTrust(oidcTrustOptions{Provider: "unknown"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestExportCIEnv(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "github_env")
	if err := os.WriteFile(envFile, []byte("EARLIER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_ENV", envFile)
	var stdout, stderr bytes.Buffer
	out := NewTestOutput(&stdout, &stderr, false, false)
	values := map[string]string{ //nolint:gosec // G101: test fixture values, not real credentials
		"DATABASE_URL": "postgres://u:p@db/x",
		"PRIVATE_KEY":  "-----BEGIN KEY-----\nabc=\n-----END KEY-----",
		"RAILS_ENV":    "production",
		"GITHUB_TOKEN": "nope",
	}
	kinds := map[string]string{"RAILS_ENV": "variable"}
	if err := exportCIEnv(out, "", values, kinds); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(envFile)
	got := string(data)
	if !strings.HasPrefix(got, "EARLIER=1\n") {
		t.Fatal("must append, not overwrite")
	}
	for _, want := range []string{"DATABASE_URL<<CLAVIS_", "\npostgres://u:p@db/x\n", "\n-----BEGIN KEY-----\nabc=\n-----END KEY-----\n", "RAILS_ENV<<CLAVIS_"} {
		if !strings.Contains(got, want) {
			t.Errorf("env file missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "GITHUB_TOKEN") {
		t.Error("reserved names must be skipped")
	}
	log := stdout.String()
	for _, want := range []string{"::add-mask::postgres://u:p@db/x", "::add-mask::abc=", "::add-mask::-----BEGIN KEY-----"} {
		if !strings.Contains(log, want) {
			t.Errorf("missing mask %q", want)
		}
	}
	if strings.Contains(log, "::add-mask::production") {
		t.Error("variables are not masked")
	}

	t.Setenv("GITHUB_ENV", "")
	if err := exportCIEnv(out, "", values, kinds); err == nil {
		t.Fatal("want an error outside a CI job")
	}
}
