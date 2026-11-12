package importers

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type gcpServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func loadGCP(ctx context.Context, opts Options) ([]Entry, error) {
	if strings.TrimSpace(opts.GCPProject) == "" {
		return nil, fmt.Errorf("GCP project is required")
	}
	if strings.TrimSpace(opts.GCPServiceAccount) == "" {
		return nil, fmt.Errorf("GCP service account JSON is required")
	}
	var account gcpServiceAccount
	if err := json.Unmarshal([]byte(opts.GCPServiceAccount), &account); err != nil {
		return nil, fmt.Errorf("decode GCP service account: %w", err)
	}
	if account.ClientEmail == "" || account.PrivateKey == "" {
		return nil, fmt.Errorf("GCP service account needs client_email and private_key")
	}
	token, err := gcpAccessToken(ctx, opts, account)
	if err != nil {
		return nil, err
	}
	base := "https://secretmanager.googleapis.com/v1/projects/" + url.PathEscape(opts.GCPProject)
	if opts.BaseURL != "" {
		base = strings.TrimRight(opts.BaseURL, "/") + "/v1/projects/" + url.PathEscape(opts.GCPProject)
	}
	nextPage := ""
	var entries []Entry
	for {
		endpoint := base + "/secrets?pageSize=100"
		if nextPage != "" {
			endpoint += "&pageToken=" + url.QueryEscape(nextPage)
		}
		var list struct {
			Secrets []struct {
				Name string `json:"name"`
			} `json:"secrets"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := getJSON(ctx, opts, endpoint, token, &list); err != nil {
			return nil, err
		}
		for _, secret := range list.Secrets {
			short := secret.Name[strings.LastIndex(secret.Name, "/")+1:]
			if opts.GCPPrefix != "" && !strings.HasPrefix(short, opts.GCPPrefix) {
				continue
			}
			keyName := strings.TrimPrefix(short, opts.GCPPrefix)
			keyName = strings.TrimPrefix(keyName, "_")
			if keyName == "" {
				keyName = short
			}
			var access struct {
				Payload struct {
					Data string `json:"data"`
				} `json:"payload"`
			}
			endpoint := "https://secretmanager.googleapis.com/v1/" + secret.Name + "/versions/latest:access"
			if opts.BaseURL != "" {
				endpoint = strings.TrimRight(opts.BaseURL, "/") + "/v1/" + secret.Name + "/versions/latest:access"
			}
			if err := getJSON(ctx, opts, endpoint, token, &access); err != nil {
				return nil, err
			}
			decoded, err := base64.StdEncoding.DecodeString(access.Payload.Data)
			if err != nil {
				return nil, fmt.Errorf("decode GCP secret %s: %w", short, err)
			}
			entries = append(entries, Entry{Key: keyName, Value: string(decoded), Kind: "secret", Source: "gcp"})
		}
		if list.NextPageToken == "" {
			break
		}
		nextPage = list.NextPageToken
	}
	return entries, nil
}

func gcpAccessToken(ctx context.Context, opts Options, account gcpServiceAccount) (string, error) {
	tokenURI := account.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}
	now := time.Now().Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss":   account.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   tokenURI,
		"iat":   now,
		"exp":   now + 3600,
	})
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("GCP private key is not PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse GCP private key: %w", err)
	}
	privateKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("GCP private key is not RSA")
	}
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign GCP JWT: %w", err)
	}
	assertion := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := httpClient(opts).Do(request)
	if err != nil {
		return "", fmt.Errorf("request GCP token: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("GCP token endpoint returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("decode GCP token: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("GCP token response did not include access_token")
	}
	return token.AccessToken, nil
}
