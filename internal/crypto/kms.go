package crypto

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/netguard"
)

// KMSConfig configures a KMS-backed root key provider. The local key file
// stores only KMS ciphertext; the plaintext key never leaves the process.
type KMSConfig struct {
	Path         string
	Provider     string // aws-kms | gcp-kms | azure-kv
	AWSRegion    string
	AWSKeyID     string
	AWSAccessKey string
	AWSSecretKey string
	AWSSession   string
	AWSEndpoint  string
	GCPKey       string
	GCPSAFile    string
	GCPEndpoint  string

	AzureVaultURL           string
	AzureKey                string
	AzureKeyVersion         string
	AzureTenantID           string
	AzureClientID           string
	AzureFederatedTokenFile string
	AzureLoginEndpoint      string

	// azureIMDSEndpoint overrides the instance metadata endpoint in tests.
	azureIMDSEndpoint string
}

// KMSProvider loads a root data key protected by AWS KMS, GCP KMS, or Azure
// Key Vault.
type KMSProvider struct {
	cfg    KMSConfig
	client *http.Client
}

const azureKeyVaultScope = "https://vault.azure.net"

// NewKMSProvider builds a KMS root key provider.
func NewKMSProvider(cfg KMSConfig) *KMSProvider {
	provider := &KMSProvider{cfg: cfg}
	switch cfg.Provider {
	case "aws-kms", "gcp-kms", "azure-kv":
	default:
		return provider
	}
	allow := []string(nil)
	endpoints := []string{
		cfg.AWSEndpoint, cfg.GCPEndpoint, cfg.AzureVaultURL, cfg.AzureLoginEndpoint,
		cfg.azureIMDSEndpoint, os.Getenv("IDENTITY_ENDPOINT"),
	}
	for _, endpoint := range endpoints {
		if strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "localhost") {
			allow = []string{"127.0.0.1/32", "::1/128"}
		}
	}
	if cfg.Provider == "azure-kv" {
		// Managed identity authenticates against the link-local metadata service.
		allow = append(allow, "169.254.169.254/32")
	}
	guardian, err := netguard.New(allow)
	if err == nil {
		provider.client = guardian.Client()
	} else {
		provider.client = &http.Client{Timeout: 20 * time.Second}
	}
	return provider
}

// Name implements Provider.
func (p *KMSProvider) Name() string { return p.cfg.Provider }

// Close implements Provider.
func (p *KMSProvider) Close() error { return nil }

// Load returns the local data key, creating and wrapping it on first use.
func (p *KMSProvider) Load(ctx context.Context) (Key, error) {
	switch p.cfg.Provider {
	case "aws-kms":
		if p.cfg.AWSRegion == "" || p.cfg.AWSKeyID == "" || p.cfg.AWSAccessKey == "" || p.cfg.AWSSecretKey == "" {
			return Key{}, fmt.Errorf("crypto: AWS KMS requires region, key id, access key, and secret key")
		}
	case "gcp-kms":
		if p.cfg.GCPKey == "" || p.cfg.GCPSAFile == "" {
			return Key{}, fmt.Errorf("crypto: GCP KMS requires a key resource name and service account file")
		}
	case "azure-kv":
		if p.cfg.AzureVaultURL == "" || p.cfg.AzureKey == "" {
			return Key{}, fmt.Errorf("crypto: Azure Key Vault requires a vault URL and key name")
		}
		if p.cfg.AzureFederatedTokenFile != "" && (p.cfg.AzureTenantID == "" || p.cfg.AzureClientID == "") {
			return Key{}, fmt.Errorf("crypto: Azure workload identity requires a tenant ID, client ID, and federated token file")
		}
		if p.cfg.AzureTenantID != "" && p.cfg.AzureFederatedTokenFile == "" {
			return Key{}, fmt.Errorf("crypto: Azure tenant ID requires a federated token file; remove it to use managed identity")
		}
		if parsed, err := url.Parse(p.cfg.AzureVaultURL); err != nil || parsed.Host == "" {
			return Key{}, fmt.Errorf("crypto: invalid Azure Key Vault URL %q", p.cfg.AzureVaultURL)
		}
	default:
		return Key{}, fmt.Errorf("crypto: unsupported KMS provider %q", p.cfg.Provider)
	}
	if p.cfg.Path == "" {
		return Key{}, fmt.Errorf("crypto: keys.root_key_file is required for KMS providers")
	}
	ciphertext, err := os.ReadFile(p.cfg.Path)
	if err == nil {
		return p.decrypt(ctx, strings.TrimSpace(string(ciphertext)))
	}
	if !os.IsNotExist(err) {
		return Key{}, fmt.Errorf("crypto: read wrapped root key: %w", err)
	}
	key, err := GenerateKey()
	if err != nil {
		return Key{}, err
	}
	encoded, err := p.encrypt(ctx, key)
	if err != nil {
		key.Zero()
		return Key{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p.cfg.Path), 0o700); err != nil {
		key.Zero()
		return Key{}, fmt.Errorf("crypto: create key directory: %w", err)
	}
	if err := os.WriteFile(p.cfg.Path, []byte(string(encoded)+"\n"), 0o600); err != nil {
		key.Zero()
		return Key{}, fmt.Errorf("crypto: write wrapped root key: %w", err)
	}
	if err := os.Chmod(p.cfg.Path, 0o600); err != nil {
		key.Zero()
		return Key{}, err
	}
	return key, nil
}

func (p *KMSProvider) encrypt(ctx context.Context, key Key) ([]byte, error) {
	switch p.cfg.Provider {
	case "aws-kms":
		return p.awsKMS(ctx, "Encrypt", key[:])
	case "azure-kv":
		wrapped, kid, err := p.azureKV(ctx, "wrapkey", p.cfg.AzureKeyVersion, key[:])
		if err != nil {
			return nil, err
		}
		// Keep the key id: Key Vault needs the exact key version to unwrap, so
		// rotating the Key Vault key must not lock the data out.
		record, err := json.Marshal(azureWrappedKey{KeyID: kid, Value: base64.StdEncoding.EncodeToString(wrapped)})
		if err != nil {
			return nil, err
		}
		return record, nil
	default:
		return p.gcpKMS(ctx, "encrypt", key[:])
	}
}

type azureWrappedKey struct {
	KeyID string `json:"kid"`
	Value string `json:"value"`
}

func (p *KMSProvider) decrypt(ctx context.Context, encoded string) (Key, error) {
	if p.cfg.Provider == "azure-kv" {
		return p.azureUnwrap(ctx, encoded)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Key{}, fmt.Errorf("crypto: invalid wrapped root key")
	}
	var plaintext []byte
	switch p.cfg.Provider {
	case "aws-kms":
		plaintext, err = p.awsKMS(ctx, "Decrypt", ciphertext)
	default:
		plaintext, err = p.gcpKMS(ctx, "decrypt", ciphertext)
	}
	if err != nil {
		return Key{}, err
	}
	return NewKeyFromBytes(plaintext)
}

func (p *KMSProvider) azureUnwrap(ctx context.Context, encoded string) (Key, error) {
	var record azureWrappedKey
	if err := json.Unmarshal([]byte(encoded), &record); err != nil || record.Value == "" {
		return Key{}, fmt.Errorf("crypto: invalid wrapped root key")
	}
	ciphertext, err := decodeAzureValue(record.Value)
	if err != nil {
		return Key{}, err
	}
	return p.azureUnwrapValue(ctx, record.KeyID, ciphertext)
}

func (p *KMSProvider) azureUnwrapValue(ctx context.Context, kid string, ciphertext []byte) (Key, error) {
	version := p.cfg.AzureKeyVersion
	if kid != "" {
		version = azureKeyVersion(kid, p.cfg.AzureKey, version)
	}
	plaintext, _, err := p.azureKV(ctx, "unwrapkey", version, ciphertext)
	if err != nil {
		return Key{}, err
	}
	return NewKeyFromBytes(plaintext)
}

// azureKeyVersion extracts the key version from a Key Vault key id such as
// https://vault.vault.azure.net/keys/name/version.
func azureKeyVersion(kid, keyName, fallback string) string {
	parsed, err := url.Parse(kid)
	if err != nil {
		return fallback
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "keys" {
		return fallback
	}
	name, nameErr := url.PathUnescape(parts[1])
	version, versionErr := url.PathUnescape(parts[2])
	if nameErr != nil || versionErr != nil || version == "" {
		return fallback
	}
	if keyName != "" && name != keyName {
		return fallback
	}
	return version
}

func (p *KMSProvider) awsKMS(ctx context.Context, operation string, input []byte) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{
		"KeyId": p.cfg.AWSKeyID, "Plaintext": base64.StdEncoding.EncodeToString(input),
	})
	if operation == "Decrypt" {
		payload, _ = json.Marshal(map[string]string{
			"KeyId": p.cfg.AWSKeyID, "CiphertextBlob": base64.StdEncoding.EncodeToString(input),
		})
	}
	endpoint := p.cfg.AWSEndpoint
	if endpoint == "" {
		endpoint = "https://kms." + p.cfg.AWSRegion + ".amazonaws.com/"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", "TrentService."+operation)
	signAWSRequest(request, payload, "kms", p.cfg.AWSRegion, p.cfg.AWSAccessKey, p.cfg.AWSSecretKey, p.cfg.AWSSession)
	response, err := p.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("crypto: AWS KMS request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("crypto: AWS KMS %s returned %d: %s", operation, response.StatusCode, strings.TrimSpace(string(body)))
	}
	if operation == "Encrypt" {
		var result struct {
			CiphertextBlob string `json:"CiphertextBlob"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		encoded, err := base64.StdEncoding.DecodeString(result.CiphertextBlob)
		if err != nil {
			return nil, err
		}
		return []byte(base64.StdEncoding.EncodeToString(encoded)), nil
	}
	var result struct {
		Plaintext string `json:"Plaintext"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(result.Plaintext)
}

type gcpServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func (p *KMSProvider) gcpKMS(ctx context.Context, operation string, input []byte) ([]byte, error) {
	raw, err := os.ReadFile(p.cfg.GCPSAFile)
	if err != nil {
		return nil, fmt.Errorf("crypto: read GCP service account: %w", err)
	}
	var account gcpServiceAccount
	if err := json.Unmarshal(raw, &account); err != nil {
		return nil, fmt.Errorf("crypto: invalid GCP service account JSON")
	}
	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}
	token, err := gcpToken(ctx, p.httpClient(), account)
	if err != nil {
		return nil, err
	}
	endpoint := p.cfg.GCPEndpoint
	if endpoint == "" {
		endpoint = "https://cloudkms.googleapis.com"
	}
	field := "plaintext"
	if operation == "decrypt" {
		field = "ciphertext"
	}
	payload, _ := json.Marshal(map[string]string{field: base64.StdEncoding.EncodeToString(input)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(endpoint, "/")+"/v1/"+p.cfg.GCPKey+":"+operation, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("crypto: GCP KMS request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("crypto: GCP KMS %s returned %d: %s", operation, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Ciphertext string `json:"ciphertext"`
		Plaintext  string `json:"plaintext"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if operation == "encrypt" {
		decoded, err := base64.StdEncoding.DecodeString(result.Ciphertext)
		if err != nil {
			return nil, err
		}
		return []byte(base64.StdEncoding.EncodeToString(decoded)), nil
	}
	return base64.StdEncoding.DecodeString(result.Plaintext)
}

func (p *KMSProvider) azureKV(ctx context.Context, operation, keyVersion string, input []byte) ([]byte, string, error) {
	token, err := p.azureToken(ctx)
	if err != nil {
		return nil, "", err
	}
	endpoint := strings.TrimRight(p.cfg.AzureVaultURL, "/") +
		"/keys/" + url.PathEscape(p.cfg.AzureKey) + "/" + url.PathEscape(keyVersion) +
		"/" + operation + "?api-version=7.4"
	payload, _ := json.Marshal(map[string]string{
		"alg":   "RSA-OAEP-256",
		"value": base64.RawURLEncoding.EncodeToString(input),
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.httpClient().Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("crypto: Azure Key Vault request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("crypto: Azure Key Vault %s returned %d: %s", operation, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		KeyID string `json:"kid"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", err
	}
	value, err := decodeAzureValue(result.Value)
	if err != nil {
		return nil, "", err
	}
	return value, result.KeyID, nil
}

// decodeAzureValue accepts both base64url (what the API returns) and standard
// base64 (what some docs and older clients show).
func decodeAzureValue(encoded string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding,
	} {
		if decoded, err := encoding.DecodeString(encoded); err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("crypto: Azure Key Vault returned an invalid value")
}

func (p *KMSProvider) azureToken(ctx context.Context) (string, error) {
	if p.cfg.AzureFederatedTokenFile != "" {
		return p.azureWorkloadIdentityToken(ctx)
	}
	if endpoint, header := os.Getenv("IDENTITY_ENDPOINT"), os.Getenv("IDENTITY_HEADER"); endpoint != "" && header != "" {
		return p.azureIdentityEndpointToken(ctx, endpoint, header)
	}
	return p.azureManagedIdentityToken(ctx)
}

func (p *KMSProvider) azureWorkloadIdentityToken(ctx context.Context) (string, error) {
	raw, err := os.ReadFile(p.cfg.AzureFederatedTokenFile)
	if err != nil {
		return "", fmt.Errorf("crypto: read Azure federated token: %w", err)
	}
	assertion := strings.TrimSpace(string(raw))
	if assertion == "" {
		return "", fmt.Errorf("crypto: Azure federated token file is empty")
	}
	login := p.cfg.AzureLoginEndpoint
	if login == "" {
		login = "https://login.microsoftonline.com"
	}
	form := url.Values{}
	form.Set("client_id", p.cfg.AzureClientID)
	form.Set("scope", azureKeyVaultScope+"/.default")
	form.Set("grant_type", "client_credentials")
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)
	endpoint := strings.TrimRight(login, "/") + "/" + url.PathEscape(p.cfg.AzureTenantID) + "/oauth2/v2.0/token"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return p.azureTokenResponse(request)
}

func (p *KMSProvider) azureIdentityEndpointToken(ctx context.Context, endpoint, header string) (string, error) {
	query := url.Values{}
	query.Set("api-version", "2019-08-01")
	query.Set("resource", azureKeyVaultScope)
	if p.cfg.AzureClientID != "" {
		query.Set("client_id", p.cfg.AzureClientID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(endpoint, "/")+"?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("X-IDENTITY-HEADER", header)
	return p.azureTokenResponse(request)
}

func (p *KMSProvider) azureManagedIdentityToken(ctx context.Context) (string, error) {
	endpoint := p.cfg.azureIMDSEndpoint
	if endpoint == "" {
		endpoint = "http://169.254.169.254"
	}
	query := url.Values{}
	query.Set("api-version", "2018-02-01")
	query.Set("resource", azureKeyVaultScope)
	if p.cfg.AzureClientID != "" {
		query.Set("client_id", p.cfg.AzureClientID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(endpoint, "/")+"/metadata/identity/oauth2/token?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Metadata", "true")
	return p.azureTokenResponse(request)
}

func (p *KMSProvider) azureTokenResponse(request *http.Request) (string, error) {
	response, err := p.httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("crypto: Azure identity request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("crypto: Azure identity endpoint returned %d", response.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return "", fmt.Errorf("crypto: Azure identity response missing access_token")
	}
	return token.AccessToken, nil
}

func (p *KMSProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

func gcpToken(ctx context.Context, client *http.Client, account gcpServiceAccount) (string, error) {
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("crypto: invalid GCP private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if key, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes); rsaErr == nil {
			parsed = key
		} else {
			return "", fmt.Errorf("crypto: invalid GCP private key")
		}
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("crypto: GCP key must be RSA")
	}
	now := time.Now().Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss": account.ClientEmail, "scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud": account.TokenURI, "iat": now, "exp": now + 3600,
	})
	payload := base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(header + "." + payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	assertion := header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("crypto: GCP token exchange failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("crypto: GCP token exchange returned %d", response.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return "", fmt.Errorf("crypto: GCP token response missing access_token")
	}
	return token.AccessToken, nil
}

func signAWSRequest(req *http.Request, payload []byte, service, region, accessKey, secretKey, sessionToken string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	hash := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(hash[:])
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", hashHex)
	headers := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-target"}
	canonical := "content-type:" + req.Header.Get("Content-Type") + "\n" +
		"host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + hashHex + "\n" +
		"x-amz-date:" + amzDate + "\n" +
		"x-amz-target:" + req.Header.Get("X-Amz-Target") + "\n"
	if sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", sessionToken)
		headers = append(headers, "x-amz-security-token")
		canonical += "x-amz-security-token:" + sessionToken + "\n"
	}
	sortStrings(headers)
	signedHeaders := strings.Join(headers, ";")
	canonicalRequest := strings.Join([]string{
		http.MethodPost, req.URL.EscapedPath(), "", canonical, signedHeaders, hashHex,
	}, "\n")
	scope := date + "/" + region + "/" + service + "/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	key := hmacBytes([]byte("AWS4"+secretKey), date)
	key = hmacBytes(key, region)
	key = hmacBytes(key, service)
	key = hmacBytes(key, "aws4_request")
	signature := hex.EncodeToString(hmacBytes(key, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacBytes(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
