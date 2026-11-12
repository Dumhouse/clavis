package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type rotationPostgresConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
	Role     string `json:"role"`
}

type rotationAWSConfig struct {
	Region       string `json:"region"`
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token,omitempty"`
	UserName     string `json:"user_name"`
}

type rotationWebhookConfig struct {
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

type rotationResult struct {
	Value string
	NewID string
}

func (s *Service) rotateCreate(ctx context.Context, provider, rawConfig, oldValue string) (*rotationResult, error) {
	switch provider {
	case "postgres":
		var config rotationPostgresConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return nil, err
		}
		passwordBytes := make([]byte, 24)
		if _, err := rand.Read(passwordBytes); err != nil {
			return nil, err
		}
		password := base64.RawURLEncoding.EncodeToString(passwordBytes)
		if err := s.setPostgresPassword(ctx, config, password); err != nil {
			return nil, err
		}
		return &rotationResult{Value: password}, nil
	case "aws":
		var config rotationAWSConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return nil, err
		}
		accessKeyID, secret, err := s.createIAMAccessKey(ctx, config)
		if err != nil {
			return nil, err
		}
		value, _ := json.Marshal(map[string]string{
			"access_key_id": accessKeyID, "secret_access_key": secret,
		})
		return &rotationResult{Value: string(value), NewID: accessKeyID}, nil
	case "webhook":
		var config rotationWebhookConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return nil, err
		}
		value, err := s.rotationWebhook(ctx, config, map[string]any{
			"action": "create", "old_value": oldValue,
		})
		if err != nil {
			return nil, err
		}
		return &rotationResult{Value: value}, nil
	default:
		return nil, fmt.Errorf("unsupported rotation provider %s", provider)
	}
}

func (s *Service) rotateRollback(ctx context.Context, provider, rawConfig, oldValue string, result *rotationResult) error {
	switch provider {
	case "postgres":
		var config rotationPostgresConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return err
		}
		if oldValue != "" {
			return s.setPostgresPassword(ctx, config, oldValue)
		}
	case "aws":
		var config rotationAWSConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return err
		}
		if result.NewID != "" {
			return s.deleteIAMAccessKey(ctx, config, result.NewID)
		}
	case "webhook":
		var config rotationWebhookConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return err
		}
		_, err := s.rotationWebhook(ctx, config, map[string]any{
			"action": "rollback", "old_value": oldValue, "new_value": result.Value,
		})
		return err
	}
	return nil
}

func (s *Service) rotateRevokeOld(ctx context.Context, provider, rawConfig, oldValue string, result *rotationResult) error {
	switch provider {
	case "postgres":
		return nil // ALTER ROLE invalidated the old password
	case "aws":
		var config rotationAWSConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return err
		}
		oldID := oldValue
		var stored map[string]string
		if json.Unmarshal([]byte(oldValue), &stored) == nil {
			if value := stored["access_key_id"]; value != "" {
				oldID = value
			}
		}
		if oldID != "" && oldID != result.NewID {
			return s.deleteIAMAccessKey(ctx, config, oldID)
		}
	case "webhook":
		var config rotationWebhookConfig
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return err
		}
		_, err := s.rotationWebhook(ctx, config, map[string]any{
			"action": "revoke", "old_value": oldValue, "new_value": result.Value,
		})
		return err
	}
	return nil
}

func (s *Service) setPostgresPassword(ctx context.Context, config rotationPostgresConfig, password string) error {
	if !safeRoleName(config.Role) {
		return fmt.Errorf("invalid role name")
	}
	db, err := s.postgresAdminClient(&postgresDynamicConfig{
		Host: config.Host, Port: config.Port, Database: config.Database,
		Username: config.Username, Password: config.Password, SSLMode: config.SSLMode,
	})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	statement := `ALTER ROLE "` + strings.ReplaceAll(config.Role, `"`, `""`) + //nolint:gosec // G202: Postgres cannot bind identifiers; they are quoted with embedded quotes doubled
		`" WITH PASSWORD '` + strings.ReplaceAll(password, "'", "''") + `'`
	_, err = db.ExecContext(ctx, statement)
	return err
}

func (s *Service) rotationWebhook(ctx context.Context, config rotationWebhookConfig, payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if config.Secret != "" {
		mac := hmac.New(sha256.New, []byte(config.Secret))
		mac.Write(raw)
		request.Header.Set("X-Clavis-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	response, err := s.ssoHTTPClient().Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("rotation webhook returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.Value == "" {
		return "", fmt.Errorf("rotation webhook returned no value")
	}
	return result.Value, nil
}

func (s *Service) createIAMAccessKey(ctx context.Context, config rotationAWSConfig) (string, string, error) {
	form := url.Values{}
	form.Set("Action", "CreateAccessKey")
	form.Set("Version", "2010-05-08")
	form.Set("UserName", config.UserName)
	body, err := s.iamCall(ctx, config, form)
	if err != nil {
		return "", "", err
	}
	var parsed struct {
		Result struct {
			AccessKey struct {
				AccessKeyID     string `xml:"AccessKeyId"`
				SecretAccessKey string `xml:"SecretAccessKey"`
			} `xml:"AccessKey"`
		} `xml:"CreateAccessKeyResult"`
	}
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return "", "", err
	}
	if parsed.Result.AccessKey.AccessKeyID == "" {
		return "", "", fmt.Errorf("IAM returned no access key")
	}
	return parsed.Result.AccessKey.AccessKeyID, parsed.Result.AccessKey.SecretAccessKey, nil
}

func (s *Service) deleteIAMAccessKey(ctx context.Context, config rotationAWSConfig, accessKeyID string) error {
	form := url.Values{}
	form.Set("Action", "DeleteAccessKey")
	form.Set("Version", "2010-05-08")
	form.Set("UserName", config.UserName)
	form.Set("AccessKeyId", accessKeyID)
	_, err := s.iamCall(ctx, config, form)
	return err
}

func (s *Service) iamCall(ctx context.Context, config rotationAWSConfig, form url.Values) ([]byte, error) {
	payload := []byte(form.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://iam.amazonaws.com/", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	signAWSQuery(request, payload, config.Region, "iam", config)
	response, err := s.ssoHTTPClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("IAM returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func signAWSQuery(request *http.Request, payload []byte, region, service string, config rotationAWSConfig) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	hash := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(hash[:])
	request.Header.Set("X-Amz-Date", amzDate)
	request.Header.Set("X-Amz-Content-Sha256", hashHex)
	headers := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date"}
	canonical := "content-type:" + request.Header.Get("Content-Type") + "\n" +
		"host:" + request.URL.Host + "\n" +
		"x-amz-content-sha256:" + hashHex + "\n" +
		"x-amz-date:" + amzDate + "\n"
	if config.SessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", config.SessionToken)
		headers = append(headers, "x-amz-security-token")
		canonical += "x-amz-security-token:" + config.SessionToken + "\n"
	}
	sort.Strings(headers)
	signedHeaders := strings.Join(headers, ";")
	canonicalRequest := strings.Join([]string{
		http.MethodPost, "/", "", canonical, signedHeaders, hashHex,
	}, "\n")
	scope := date + "/" + region + "/" + service + "/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	key := hmacSHA256Bytes([]byte("AWS4"+config.SecretKey), date)
	key = hmacSHA256Bytes(key, region)
	key = hmacSHA256Bytes(key, service)
	key = hmacSHA256Bytes(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256Bytes(key, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+config.AccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}
