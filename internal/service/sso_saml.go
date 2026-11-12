package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"

	"github.com/dumhouse/clavis/internal/store"
)

type samlTicket struct {
	OrgID      string
	RequestID  string
	ReturnPath string
	Expires    time.Time
}

type samlTicketStore struct {
	mu     sync.Mutex
	now    func() time.Time
	byOrg  map[string]samlTicket
	replay map[string]time.Time
}

func newSAMLTicketStore(now func() time.Time) *samlTicketStore {
	return &samlTicketStore{now: now, byOrg: make(map[string]samlTicket), replay: make(map[string]time.Time)}
}

func (s *samlTicketStore) put(ticket samlTicket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	ticket.Expires = s.now().UTC().Add(15 * time.Minute)
	s.byOrg[ticket.OrgID] = ticket
}

func (s *samlTicketStore) take(orgID string) (samlTicket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.byOrg[orgID]
	delete(s.byOrg, orgID)
	if !ok || s.now().UTC().After(ticket.Expires) {
		return samlTicket{}, false
	}
	return ticket, true
}

// Consume implements saml.AssertionReplayCache.
func (s *samlTicketStore) Consume(assertionID string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if _, ok := s.replay[assertionID]; ok {
		return saml.ErrAssertionReplayed
	}
	s.replay[assertionID] = expiresAt
	return nil
}

func (s *samlTicketStore) pruneLocked() {
	now := s.now().UTC()
	for id, expiry := range s.replay {
		if now.After(expiry) {
			delete(s.replay, id)
		}
	}
	for org, ticket := range s.byOrg {
		if now.After(ticket.Expires) {
			delete(s.byOrg, org)
		}
	}
}

func generateSAMLKeypair() (string, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Clavis SAML"},
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return string(certPEM), string(keyPEM), nil
}

func parseSAMLKeypair(certPEM, keyPEM string) (*rsa.PrivateKey, *x509.Certificate, error) {
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("%w: invalid SAML private key", ErrSSOUnavailable)
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err == nil {
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, nil, fmt.Errorf("%w: SAML key must be RSA", ErrSSOUnavailable)
		}
		key = rsaKey
	} else if parsed, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err == nil {
		key = parsed
	} else {
		return nil, nil, fmt.Errorf("%w: invalid SAML private key", ErrSSOUnavailable)
	}
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		return nil, nil, fmt.Errorf("%w: invalid SAML certificate", ErrSSOUnavailable)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: invalid SAML certificate", ErrSSOUnavailable)
	}
	return key, cert, nil
}

// SAMLServiceProvider builds the SP for an org from stored settings.
func (s *Service) SAMLServiceProvider(ctx context.Context, orgSlug, baseURL string) (*saml.ServiceProvider, *store.Org, OrgSettings, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, OrgSettings{}, ErrNotFound
		}
		return nil, nil, OrgSettings{}, err
	}
	settings := ParseOrgSettings(org.Settings)
	if !settings.SSOEnabled || settings.SSOProvider != "saml" || settings.SSOIdPMetadata == "" {
		return nil, nil, OrgSettings{}, ErrSSOUnavailable
	}
	keyPEM, err := s.decryptSSOSecret(org, settings.SSOSPPrivateKey)
	if err != nil {
		return nil, nil, OrgSettings{}, err
	}
	key, cert, err := parseSAMLKeypair(settings.SSOSPCertificate, keyPEM)
	if err != nil {
		return nil, nil, OrgSettings{}, err
	}
	metadataXML, err := s.fetchSAMLMetadata(ctx, settings.SSOIdPMetadata)
	if err != nil {
		return nil, nil, OrgSettings{}, err
	}
	var idp saml.EntityDescriptor
	if err := xml.Unmarshal(metadataXML, &idp); err != nil {
		return nil, nil, OrgSettings{}, fmt.Errorf("%w: invalid IdP metadata", ErrSSOUnavailable)
	}
	entityID := strings.TrimRight(baseURL, "/") + "/saml/" + url.PathEscape(org.Slug) + "/metadata"
	metadataURL, _ := url.Parse(entityID)
	acsURL, _ := url.Parse(strings.TrimRight(baseURL, "/") + "/saml/" + url.PathEscape(org.Slug) + "/acs")
	sp := &saml.ServiceProvider{
		EntityID:    entityID,
		Key:         key,
		Certificate: cert,
		MetadataURL: *metadataURL,
		AcsURL:      *acsURL,
		IDPMetadata: &idp,
		ReplayCache: s.samlTickets,
	}
	return sp, org, settings, nil
}

func (s *Service) fetchSAMLMetadata(ctx context.Context, source string) ([]byte, error) {
	if strings.HasPrefix(source, "<") {
		return []byte(source), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid IdP metadata URL", ErrSSOUnavailable)
	}
	resp, err := s.ssoHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: could not fetch IdP metadata", ErrSSOUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: IdP metadata returned %d", ErrSSOUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: could not read IdP metadata", ErrSSOUnavailable)
	}
	return body, nil
}

// BeginSAML creates a redirect URL for an HTTP-Redirect AuthnRequest.
func (s *Service) BeginSAML(ctx context.Context, orgSlug, baseURL, returnPath string) (string, error) {
	sp, org, _, err := s.SAMLServiceProvider(ctx, orgSlug, baseURL)
	if err != nil {
		return "", err
	}
	idpURL := sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if idpURL == "" {
		return "", fmt.Errorf("%w: IdP does not advertise HTTP-Redirect SSO", ErrSSOUnavailable)
	}
	returnPath = SafeReturnPath(returnPath)
	request, err := sp.MakeAuthenticationRequest(idpURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", fmt.Errorf("%w: could not build AuthnRequest", ErrSSOUnavailable)
	}
	redirect, err := request.Redirect(returnPath, sp)
	if err != nil {
		return "", fmt.Errorf("%w: could not sign AuthnRequest", ErrSSOUnavailable)
	}
	s.samlTickets.put(samlTicket{OrgID: org.ID, RequestID: request.ID, ReturnPath: returnPath})
	return redirect.String(), nil
}

// FinishSAML validates an ACS response and provisions the user.
func (s *Service) FinishSAML(ctx context.Context, orgSlug, baseURL string, r *http.Request, meta AuditMeta) (*store.User, *store.Org, string, error) {
	sp, org, settings, err := s.SAMLServiceProvider(ctx, orgSlug, baseURL)
	if err != nil {
		return nil, nil, "", err
	}
	ticket, ok := s.samlTickets.take(org.ID)
	if !ok {
		return nil, nil, "", fmt.Errorf("%w: no pending SAML request", ErrSSOUnavailable)
	}
	if err := r.ParseForm(); err != nil {
		return nil, nil, "", fmt.Errorf("%w: invalid SAML response", ErrSSOUnavailable)
	}
	assertion, err := sp.ParseResponse(r, []string{ticket.RequestID})
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: %w", ErrSSOUnavailable, err)
	}
	email, name := samlIdentity(assertion)
	if email == "" {
		return nil, nil, "", fmt.Errorf("%w: assertion carried no email", ErrSSOUnavailable)
	}
	normalized, err := normalizeEmail(email)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: assertion carried an invalid email", ErrSSOUnavailable)
	}
	if !ssoEmailAllowed(normalized, settings.SSOAllowedDomains) {
		return nil, nil, "", fmt.Errorf("%w: email domain is not allowed for this organization", ErrForbidden)
	}
	user, err := s.provisionSSOUser(ctx, org, settings, normalized, name, "saml", meta)
	if err != nil {
		return nil, nil, "", err
	}
	return user, org, ticket.ReturnPath, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", errors.New("empty email")
	}
	if at := strings.Index(email, "@"); at <= 0 || at == len(email)-1 {
		return "", errors.New("invalid email")
	}
	return strings.ToLower(email), nil
}

func samlIdentity(assertion *saml.Assertion) (string, string) {
	if assertion == nil {
		return "", ""
	}
	email := ""
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		email = strings.TrimSpace(assertion.Subject.NameID.Value)
	}
	name := ""
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			key := strings.ToLower(strings.TrimSpace(attribute.Name))
			value := ""
			if len(attribute.Values) > 0 {
				value = strings.TrimSpace(attribute.Values[0].Value)
			}
			if value == "" {
				continue
			}
			switch key {
			case "email", "mail", "urn:oid:0.9.2342.19200300.100.1.3",
				"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress":
				email = value
			case "displayname", "name", "cn", "urn:oid:2.16.840.1.113730.3.1.241",
				"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name":
				name = value
			}
		}
	}
	return email, name
}
