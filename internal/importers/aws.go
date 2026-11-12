package importers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

func loadAWS(ctx context.Context, opts Options) ([]Entry, error) {
	if opts.AWSRegion == "" || opts.AWSAccessKey == "" || opts.AWSSecretKey == "" {
		return nil, fmt.Errorf("AWS region, access key, and secret key are required")
	}
	endpoint := "https://secretsmanager." + opts.AWSRegion + ".amazonaws.com"
	if opts.BaseURL != "" {
		endpoint = strings.TrimRight(opts.BaseURL, "/")
	}
	var entries []Entry
	nextToken := ""
	for {
		payload := map[string]any{}
		if nextToken != "" {
			payload["NextToken"] = nextToken
		}
		body, err := awsCall(ctx, opts, endpoint, "secretsmanager.ListSecrets", payload)
		if err != nil {
			return nil, err
		}
		var list struct {
			SecretList []struct {
				Name string `json:"Name"`
			} `json:"SecretList"`
			NextToken string `json:"NextToken"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, fmt.Errorf("decode AWS Secrets Manager list: %w", err)
		}
		for _, secret := range list.SecretList {
			if opts.AWSPrefix != "" && !strings.HasPrefix(secret.Name, opts.AWSPrefix) {
				continue
			}
			valueBody, err := awsCall(ctx, opts, endpoint, "secretsmanager.GetSecretValue", map[string]any{
				"SecretId": secret.Name,
			})
			if err != nil {
				return nil, err
			}
			var value struct {
				Name         string `json:"Name"`
				SecretString string `json:"SecretString"`
				SecretBinary string `json:"SecretBinary"`
			}
			if err := json.Unmarshal(valueBody, &value); err != nil {
				return nil, fmt.Errorf("decode AWS secret %s: %w", secret.Name, err)
			}
			keyName := strings.TrimPrefix(secret.Name, opts.AWSPrefix)
			keyName = strings.TrimPrefix(keyName, "/")
			raw := value.SecretString
			if raw == "" && value.SecretBinary != "" {
				decoded, err := base64.StdEncoding.DecodeString(value.SecretBinary)
				if err != nil {
					return nil, fmt.Errorf("decode AWS binary secret %s: %w", secret.Name, err)
				}
				raw = string(decoded)
			}
			if mapped, ok := keyValue(raw); ok {
				for key, inner := range mapped {
					entries = append(entries, Entry{Key: key, Value: inner, Kind: opts.AWSKind, Source: "aws"})
				}
				continue
			}
			if keyName == "" {
				continue
			}
			entries = append(entries, Entry{Key: keyName, Value: raw, Kind: opts.AWSKind, Source: "aws"})
		}
		if list.NextToken == "" {
			break
		}
		nextToken = list.NextToken
	}
	return entries, nil
}

func awsCall(ctx context.Context, opts Options, endpoint, target string, payload map[string]any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", target)
	signAWS(request, raw, opts)
	response, err := httpClient(opts).Do(request)
	if err != nil {
		return nil, fmt.Errorf("AWS request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("AWS %s returned %d: %s", target, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func signAWS(request *http.Request, payload []byte, opts Options) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	hash := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(hash[:])
	request.Header.Set("X-Amz-Date", amzDate)
	request.Header.Set("X-Amz-Content-Sha256", hashHex)
	headers := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-target"}
	canonical := "content-type:" + request.Header.Get("Content-Type") + "\n" +
		"host:" + request.URL.Host + "\n" +
		"x-amz-content-sha256:" + hashHex + "\n" +
		"x-amz-date:" + amzDate + "\n" +
		"x-amz-target:" + request.Header.Get("X-Amz-Target") + "\n"
	if opts.AWSSessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", opts.AWSSessionToken)
		headers = append(headers, "x-amz-security-token")
		canonical += "x-amz-security-token:" + opts.AWSSessionToken + "\n"
	}
	sort.Strings(headers)
	signedHeaders := strings.Join(headers, ";")
	canonicalRequest := strings.Join([]string{
		http.MethodPost, request.URL.EscapedPath(), "", canonical, signedHeaders, hashHex,
	}, "\n")
	scope := date + "/" + opts.AWSRegion + "/secretsmanager/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	key := hmacSHA256([]byte("AWS4"+opts.AWSSecretKey), date)
	key = hmacSHA256(key, opts.AWSRegion)
	key = hmacSHA256(key, "secretsmanager")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+opts.AWSAccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
