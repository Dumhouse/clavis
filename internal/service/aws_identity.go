package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// AWSRequest is a signed STS GetCallerIdentity request replayed by the server.
type AWSRequest struct {
	Authorization string
	AMZDate       string
	SecurityToken string
	ContentType   string
	ContentSHA256 string
	Host          string
}

type stsIdentityResponse struct {
	Result struct {
		Arn     string `xml:"Arn"`
		UserID  string `xml:"UserId"`
		Account string `xml:"Account"`
	} `xml:"GetCallerIdentityResult"`
}

// ExchangeAWS verifies a SigV4-signed GetCallerIdentity request with STS and
// issues a short-lived Clavis token.
func (s *Service) ExchangeAWS(ctx context.Context, orgSlug, identityName string, in AWSRequest, meta AuditMeta) (*IdentityResult, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		return nil, ErrNotFound
	}
	identity, err := s.Store.GetIdentityByName(ctx, org.ID, strings.TrimSpace(identityName))
	if err != nil {
		if err == store.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var trust AWSTrust
	if identity.AWSTrust == "" || identity.AWSTrust == "{}" {
		return nil, fmt.Errorf("%w: identity has no AWS trust configured", ErrForbidden)
	}
	if err := json.Unmarshal([]byte(identity.AWSTrust), &trust); err != nil {
		return nil, fmt.Errorf("%w: invalid AWS trust", ErrForbidden)
	}
	if !strings.HasPrefix(in.Authorization, "AWS4-HMAC-SHA256 ") || in.AMZDate == "" {
		return nil, fmt.Errorf("%w: missing AWS SigV4 headers", ErrForbidden)
	}
	endpoint := s.AWSSTSEndpoint
	if endpoint == "" {
		endpoint = "https://sts.amazonaws.com/"
	}
	host := in.Host
	if host == "" {
		host = "sts.amazonaws.com"
	}
	contentType := in.ContentType
	if contentType == "" {
		contentType = "application/x-www-form-urlencoded; charset=utf-8"
	}
	body := "Action=GetCallerIdentity&Version=2011-06-15"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Host = host
	request.Header.Set("Authorization", in.Authorization)
	request.Header.Set("X-Amz-Date", in.AMZDate)
	request.Header.Set("Content-Type", contentType)
	if in.SecurityToken != "" {
		request.Header.Set("X-Amz-Security-Token", in.SecurityToken)
	}
	if in.ContentSHA256 != "" {
		request.Header.Set("X-Amz-Content-Sha256", in.ContentSHA256)
	}
	response, err := s.ssoHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: STS request failed; check the network", ErrSSOUnavailable)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: AWS rejected the signature", ErrForbidden)
	}
	var result stsIdentityResponse
	if err := xml.Unmarshal(responseBody, &result); err != nil || result.Result.Account == "" {
		return nil, fmt.Errorf("%w: invalid STS response", ErrSSOUnavailable)
	}
	if trust.Account != "" && trust.Account != result.Result.Account {
		return nil, fmt.Errorf("%w: AWS account does not match", ErrForbidden)
	}
	if trust.ARNPattern != "" && !authz.MatchGlob(trust.ARNPattern, result.Result.Arn) {
		return nil, fmt.Errorf("%w: AWS ARN does not match", ErrForbidden)
	}
	if trust.UserIDPattern != "" && !authz.MatchGlob(trust.UserIDPattern, result.Result.UserID) {
		return nil, fmt.Errorf("%w: AWS user ID does not match", ErrForbidden)
	}
	scopes := tokenScopes{Role: authz.RoleDeveloper, Environment: "*"}
	tokens, err := s.Store.ListTokensForIdentity(ctx, identity.ID)
	if err != nil {
		return nil, err
	}
	if len(tokens) > 0 && tokens[0].Scopes != "" {
		_ = json.Unmarshal([]byte(tokens[0].Scopes), &scopes)
	}
	duration := time.Hour
	if identity.TokenTTLSeconds > 0 && identity.TokenTTLSeconds < 3600 {
		duration = time.Duration(identity.TokenTTLSeconds) * time.Second
	}
	if duration < 5*time.Minute {
		duration = 5 * time.Minute
	}
	token, record, err := s.newToken(ctx, identity, scopes, duration)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(authz.Actor{ID: identity.ID, Kind: authz.SubjectMachine, Name: identity.Name}),
		org.ID, "identity.aws_login", audit.Resource{Type: "identity", ID: identity.ID, Name: identity.Name},
		audit.Details{Environment: scopes.Environment, Reason: result.Result.Arn}, meta); err != nil {
		s.Logger.Error("audit identity.aws_login failed", "identity_id", identity.ID, "error", err)
	}
	return &IdentityResult{Identity: identity, Token: token, Record: record}, nil
}
