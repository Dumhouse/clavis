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
