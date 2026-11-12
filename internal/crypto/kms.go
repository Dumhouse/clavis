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
	Provider     string // aws-kms | gcp-kms
	AWSRegion    string
	AWSKeyID     string
	AWSAccessKey string
	AWSSecretKey string
	AWSSession   string
	AWSEndpoint  string
	GCPKey       string
	GCPSAFile    string
	GCPEndpoint  string
}

// KMSProvider loads a root data key protected by AWS KMS or GCP KMS.
type KMSProvider struct {
	cfg    KMSConfig
	client *http.Client
}

// NewKMSProvider builds a KMS root key provider.
func NewKMSProvider(cfg KMSConfig) *KMSProvider {
	provider := &KMSProvider{cfg: cfg}
	if provider.cfg.Provider != "aws-kms" && provider.cfg.Provider != "gcp-kms" {
		return provider
	}
	allow := []string(nil)
	for _, endpoint := range []string{cfg.AWSEndpoint, cfg.GCPEndpoint} {
		if strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "localhost") {
			allow = []string{"127.0.0.1/32", "::1/128"}
		}
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
	default:
		return p.gcpKMS(ctx, "encrypt", key[:])
	}
}

func (p *KMSProvider) decrypt(ctx context.Context, encoded string) (Key, error) {
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
