package crypto

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAWSKMSProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Errorf("missing SigV4 authorization")
		}
		body, _ := io.ReadAll(r.Body)
		target := r.Header.Get("X-Amz-Target")
		if strings.HasSuffix(target, "Encrypt") {
			var in struct {
				Plaintext string `json:"Plaintext"`
			}
			_ = json.Unmarshal(body, &in)
			raw, _ := base64.StdEncoding.DecodeString(in.Plaintext)
			ciphertext := append([]byte("kms:"), raw...)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"CiphertextBlob": base64.StdEncoding.EncodeToString(ciphertext),
			})
			return
		}
		var in struct {
			CiphertextBlob string `json:"CiphertextBlob"`
		}
		_ = json.Unmarshal(body, &in)
		raw, _ := base64.StdEncoding.DecodeString(in.CiphertextBlob)
		raw = []byte(strings.TrimPrefix(string(raw), "kms:"))
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Plaintext": base64.StdEncoding.EncodeToString(raw),
		})
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "root.kms")
	provider := NewKMSProvider(KMSConfig{
		Path: path, Provider: "aws-kms", AWSRegion: "eu-central-1", AWSKeyID: "key-1",
		AWSAccessKey: "AKID", AWSSecretKey: "SECRET", AWSEndpoint: srv.URL,
	})
	first, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("wrapped key file missing: %v", err)
	}
	second, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("KMS round-trip changed the key")
	}
}

func TestGCPKMSProvider(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	sa := map[string]string{
		"client_email": "kms@project.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			_, _ = w.Write([]byte(`{"access_token":"gcp-token"}`))
		case strings.HasSuffix(r.URL.Path, ":encrypt"):
			var in struct {
				Plaintext string `json:"plaintext"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			raw, _ := base64.StdEncoding.DecodeString(in.Plaintext)
			ciphertext := append([]byte("gcp:"), raw...)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"ciphertext": base64.StdEncoding.EncodeToString(ciphertext),
			})
		case strings.HasSuffix(r.URL.Path, ":decrypt"):
			var in struct {
				Ciphertext string `json:"ciphertext"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			raw, _ := base64.StdEncoding.DecodeString(in.Ciphertext)
			raw = []byte(strings.TrimPrefix(string(raw), "gcp:"))
			_ = json.NewEncoder(w).Encode(map[string]string{
				"plaintext": base64.StdEncoding.EncodeToString(raw),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	sa["token_uri"] = srv.URL + "/token"
	saPath := filepath.Join(t.TempDir(), "sa.json")
	encoded, _ := json.Marshal(sa)
	if err := os.WriteFile(saPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "root.kms")
	provider := NewKMSProvider(KMSConfig{
		Path: path, Provider: "gcp-kms", GCPKey: "projects/p/locations/l/keyRings/r/cryptoKeys/k",
		GCPSAFile: saPath, GCPEndpoint: srv.URL,
	})
	first, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("GCP KMS round-trip changed the key")
	}
}

func TestAzureKeyVaultWorkloadIdentity(t *testing.T) {
	t.Setenv("IDENTITY_ENDPOINT", "")
	t.Setenv("IDENTITY_HEADER", "")

	var exchanged atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			if err := r.ParseForm(); err != nil {
				t.Errorf("token form: %v", err)
			}
			if got := r.Form.Get("client_assertion"); got != "federated-assertion" {
				t.Errorf("client_assertion = %q", got)
			}
			if got := r.Form.Get("scope"); got != "https://vault.azure.net/.default" {
				t.Errorf("scope = %q", got)
			}
			if got := r.Form.Get("client_id"); got != "client-1" {
				t.Errorf("client_id = %q", got)
			}
			exchanged.Store(true)
			_, _ = w.Write([]byte(`{"access_token":"azure-token"}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer azure-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("api-version"); got != "7.4" {
			t.Errorf("api-version = %q", got)
		}
		if strings.HasSuffix(r.URL.Path, "/wrapkey") && !strings.HasPrefix(r.URL.Path, "/keys/root//") {
			t.Errorf("wrap path = %q", r.URL.Path)
		}
		if strings.HasSuffix(r.URL.Path, "/unwrapkey") && !strings.HasPrefix(r.URL.Path, "/keys/root/v9/") {
			t.Errorf("unwrap path = %q", r.URL.Path)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/wrapkey"):
			var in struct {
				Alg   string `json:"alg"`
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Alg != "RSA-OAEP-256" {
				t.Errorf("alg = %q", in.Alg)
			}
			raw, err := base64.RawURLEncoding.DecodeString(in.Value)
			if err != nil {
				t.Errorf("wrap value: %v", err)
			}
			// Standard base64 on the way back exercises the tolerant decode.
			_ = json.NewEncoder(w).Encode(map[string]string{
				"kid":   "https://vault.example.net/keys/root/v9",
				"value": base64.StdEncoding.EncodeToString(append([]byte("az:"), raw...)),
			})
		case strings.HasSuffix(r.URL.Path, "/unwrapkey"):
			var in struct {
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			raw, err := base64.RawURLEncoding.DecodeString(in.Value)
			if err != nil {
				t.Errorf("unwrap value: %v", err)
			}
			raw = []byte(strings.TrimPrefix(string(raw), "az:"))
			_ = json.NewEncoder(w).Encode(map[string]string{
				"value": base64.RawURLEncoding.EncodeToString(raw),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tokenFile := filepath.Join(t.TempDir(), "federated-token")
	if err := os.WriteFile(tokenFile, []byte("federated-assertion\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "root.kms")
	provider := NewKMSProvider(KMSConfig{
		Path: path, Provider: "azure-kv",
		AzureVaultURL: srv.URL, AzureKey: "root",
		AzureTenantID: "tenant-1", AzureClientID: "client-1",
		AzureFederatedTokenFile: tokenFile, AzureLoginEndpoint: srv.URL,
	})
	first, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !exchanged.Load() {
		t.Fatal("workload identity token exchange was not attempted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("wrapped key file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("wrapped key mode = %o", info.Mode().Perm())
	}
	second, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Azure Key Vault round-trip changed the key")
	}
}

func TestAzureKeyVaultManagedIdentity(t *testing.T) {
	t.Setenv("IDENTITY_ENDPOINT", "")
	t.Setenv("IDENTITY_HEADER", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata/identity/oauth2/token" {
			if got := r.Header.Get("Metadata"); got != "true" {
				t.Errorf("metadata header = %q", got)
			}
			query := r.URL.Query()
			if query.Get("api-version") != "2018-02-01" ||
				query.Get("resource") != "https://vault.azure.net" ||
				query.Get("client_id") != "client-1" {
				t.Errorf("imds query = %v", query)
			}
			_, _ = w.Write([]byte(`{"access_token":"azure-token"}`))
			return
		}
		azureKeyVaultRoundTrip(t, w, r)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "root.kms")
	provider := NewKMSProvider(KMSConfig{
		Path: path, Provider: "azure-kv", AzureVaultURL: srv.URL, AzureKey: "root",
		AzureKeyVersion: "v1", AzureClientID: "client-1", azureIMDSEndpoint: srv.URL,
	})
	first, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Azure managed identity round-trip changed the key")
	}
}

func TestAzureKeyVaultAppServiceIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/msi/token" {
			if got := r.Header.Get("X-IDENTITY-HEADER"); got != "header-1" {
				t.Errorf("identity header = %q", got)
			}
			query := r.URL.Query()
			if query.Get("api-version") != "2019-08-01" ||
				query.Get("resource") != "https://vault.azure.net" {
				t.Errorf("identity query = %v", query)
			}
			_, _ = w.Write([]byte(`{"access_token":"azure-token"}`))
			return
		}
		azureKeyVaultRoundTrip(t, w, r)
	}))
	defer srv.Close()

	t.Setenv("IDENTITY_ENDPOINT", srv.URL+"/msi/token")
	t.Setenv("IDENTITY_HEADER", "header-1")
	path := filepath.Join(t.TempDir(), "root.kms")
	provider := NewKMSProvider(KMSConfig{
		Path: path, Provider: "azure-kv", AzureVaultURL: srv.URL, AzureKey: "root",
		AzureKeyVersion: "v1",
	})
	if _, err := provider.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func azureKeyVaultRoundTrip(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer azure-token" {
		t.Errorf("authorization = %q", got)
	}
	if !strings.HasPrefix(r.URL.Path, "/keys/root/v1/") {
		t.Errorf("path = %q", r.URL.Path)
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/wrapkey"):
		var in struct {
			Alg   string `json:"alg"`
			Value string `json:"value"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Alg != "RSA-OAEP-256" {
			t.Errorf("alg = %q", in.Alg)
		}
		raw, err := base64.RawURLEncoding.DecodeString(in.Value)
		if err != nil {
			t.Errorf("wrap value: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"kid":   "https://vault.example.net/keys/root/v1",
			"value": base64.RawURLEncoding.EncodeToString(append([]byte("az:"), raw...)),
		})
	case strings.HasSuffix(r.URL.Path, "/unwrapkey"):
		var in struct {
			Value string `json:"value"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		raw, err := base64.RawURLEncoding.DecodeString(in.Value)
		if err != nil {
			t.Errorf("unwrap value: %v", err)
		}
		raw = []byte(strings.TrimPrefix(string(raw), "az:"))
		_ = json.NewEncoder(w).Encode(map[string]string{
			"value": base64.RawURLEncoding.EncodeToString(raw),
		})
	default:
		http.NotFound(w, r)
	}
}
