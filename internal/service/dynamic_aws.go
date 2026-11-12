package service

import (
	"bytes"
	"context"
	"crypto/sha256"
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

	"github.com/dumhouse/clavis/internal/store"
)

type awsDynamicConfig struct {
	Region       string `json:"region"`
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token,omitempty"`
	RoleARN      string `json:"role_arn"`
	ExternalID   string `json:"external_id,omitempty"`
	SessionName  string `json:"session_name,omitempty"`
}

var _ = bytes.NewReader

func validateAWSDynamic(config map[string]any) error {
	for _, key := range []string{"region", "access_key", "secret_key", "role_arn"} {
		if value, _ := config[key].(string); strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: aws config needs %s", ErrValidation, key)
		}
	}
	return nil
}

func (s *Service) createAWSCredentials(ctx context.Context, config *awsDynamicConfig, leaseID string, ttl int) (map[string]string, error) {
	region := config.Region
	if region == "" {
		region = "us-east-1"
	}
	endpoint := "https://sts." + region + ".amazonaws.com"
	sessionName := config.SessionName
	if sessionName == "" {
		sessionName = "clavis-" + strings.ReplaceAll(leaseID, "-", "")[:12]
	}
	form := url.Values{}
	form.Set("Action", "AssumeRole")
	form.Set("Version", "2011-06-15")
	form.Set("RoleArn", config.RoleARN)
	form.Set("RoleSessionName", sessionName)
	form.Set("DurationSeconds", fmt.Sprintf("%d", ttl))
	if config.ExternalID != "" {
		form.Set("ExternalId", config.ExternalID)
	}
	payload := []byte(form.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	signAWSForm(request, payload, region, config)
	response, err := s.ssoHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("dynamic aws: request failed: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("dynamic aws: STS returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		Result struct {
			Credentials struct {
				AccessKeyID     string `xml:"AccessKeyId"`
				SecretAccessKey string `xml:"SecretAccessKey"`
				SessionToken    string `xml:"SessionToken"`
				Expiration      string `xml:"Expiration"`
			} `xml:"Credentials"`
		} `xml:"AssumeRoleResult"`
	}
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("dynamic aws: decode STS response: %w", err)
	}
	credentials := parsed.Result.Credentials
	if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
		return nil, fmt.Errorf("dynamic aws: STS returned no credentials")
	}
	return map[string]string{
		"engine": "aws", "access_key_id": credentials.AccessKeyID,
		"secret_access_key": credentials.SecretAccessKey,
		"session_token":     credentials.SessionToken, "expiration": credentials.Expiration,
	}, nil
}

func signAWSForm(request *http.Request, payload []byte, region string, config *awsDynamicConfig) {
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
	scope := date + "/" + region + "/sts/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	key := hmacSHA256Bytes([]byte("AWS4"+config.SecretKey), date)
	key = hmacSHA256Bytes(key, region)
	key = hmacSHA256Bytes(key, "sts")
	key = hmacSHA256Bytes(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256Bytes(key, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+config.AccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func (s *Service) decryptAWSDynamicConfig(org *store.Org, config *store.DynamicSecretConfig) (*awsDynamicConfig, error) {
	raw, err := s.decryptOrgSecret(org, config.ID, "dynamic_config", config.Config)
	if err != nil {
		return nil, err
	}
	var out awsDynamicConfig
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
