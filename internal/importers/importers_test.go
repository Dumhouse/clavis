package importers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInfisicalAndDoppler(t *testing.T) {
	infisical := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer inf-token" {
			t.Fatalf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("workspaceId") != "ws" || r.URL.Query().Get("environment") != "dev" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]string{
			{"secretKey": "DB_URL", "secretValue": "postgres://db"},
		}})
	}))
	defer infisical.Close()
	entries, err := Load(context.Background(), Options{
		Provider: "infisical", BaseURL: infisical.URL, Token: "inf-token",
		RemoteProject: "ws", RemoteEnv: "dev",
	})
	if err != nil || len(entries) != 1 || entries[0].Value != "postgres://db" {
		t.Fatalf("infisical = %#v %v", entries, err)
	}

	doppler := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"API_KEY": "abc"})
	}))
	defer doppler.Close()
	entries, err = Load(context.Background(), Options{
		Provider: "doppler", BaseURL: doppler.URL, Token: "dop-token",
		RemoteProject: "proj", RemoteEnv: "dev",
	})
	if err != nil || len(entries) != 1 || entries[0].Key != "API_KEY" {
		t.Fatalf("doppler = %#v %v", entries, err)
	}
}

func TestVaultKV2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "vault-token" {
			t.Fatalf("token = %q", r.Header.Get("X-Vault-Token"))
		}
		if r.URL.Path != "/v1/secret/data/app" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]any{
			"DB_URL": "postgres://db", "PORT": float64(5432),
		}}})
	}))
	defer server.Close()
	entries, err := Load(context.Background(), Options{
		Provider: "vault", BaseURL: server.URL, Token: "vault-token",
		VaultMount: "secret", VaultVersion: 2, RemotePath: "app",
	})
	if err != nil || len(entries) != 2 {
		t.Fatalf("vault = %#v %v", entries, err)
	}
	if entries[1].Key != "PORT" || entries[1].Value != "5432" {
		t.Fatalf("vault port = %#v", entries[1])
	}
}

func TestOnePasswordConnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/vaults/vault/items" {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]string{{"id": "item", "title": "Database"}}})
			return
		}
		if r.URL.Path == "/v1/vaults/vault/items/item" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title":  "Database",
				"fields": []map[string]string{{"label": "username", "value": "app"}, {"label": "password", "value": "secret"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	entries, err := Load(context.Background(), Options{
		Provider: "onepassword", BaseURL: server.URL, Token: "op-token", OnePasswordVault: "vault",
	})
	if err != nil || len(entries) != 2 || entries[0].Key != "PASSWORD" {
		t.Fatalf("onepassword = %#v %v", entries, err)
	}
}

func TestAWSSecretsManager(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Fatalf("not signed: %q", r.Header.Get("Authorization"))
		}
		switch r.Header.Get("X-Amz-Target") {
		case "secretsmanager.ListSecrets":
			_ = json.NewEncoder(w).Encode(map[string]any{"SecretList": []map[string]string{{"Name": "app/DB_URL"}}})
		case "secretsmanager.GetSecretValue":
			_ = json.NewEncoder(w).Encode(map[string]any{"Name": "app/DB_URL", "SecretString": `{"DB_URL":"postgres://db"}`})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	entries, err := Load(context.Background(), Options{
		Provider: "aws", BaseURL: server.URL, AWSRegion: "eu-central-1",
		AWSAccessKey: "AKIA", AWSSecretKey: "secret", AWSPrefix: "app/", AWSKind: "secret",
	})
	if err != nil || len(entries) != 1 || entries[0].Key != "DB_URL" {
		t.Fatalf("aws = %#v %v", entries, err)
	}
}

func TestGCPSecretManager(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "" || r.Form.Get("assertion") == "" {
			t.Fatalf("token form = %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gcp-token"})
	}))
	defer tokenServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gcp-token" {
			t.Fatalf("auth = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/projects/proj/secrets":
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]string{{"name": "projects/proj/secrets/APP_KEY"}}})
		case "/v1/projects/proj/secrets/APP_KEY/versions/latest:access":
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": map[string]string{
				"data": base64.StdEncoding.EncodeToString([]byte("value")),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()
	account, _ := json.Marshal(map[string]string{
		"client_email": "bot@example.com", "private_key": pemKey, "token_uri": tokenServer.URL,
	})
	entries, err := Load(context.Background(), Options{
		Provider: "gcp", BaseURL: apiServer.URL, GCPProject: "proj", GCPServiceAccount: string(account),
	})
	if err != nil || len(entries) != 1 || entries[0].Key != "APP_KEY" || entries[0].Value != "value" {
		t.Fatalf("gcp = %#v %v", entries, err)
	}
}
