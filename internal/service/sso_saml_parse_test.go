package service

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/crewjam/saml"
)

func TestParseSAMLResponseRejectsGarbage(t *testing.T) {
	form := url.Values{"SAMLResponse": {"bm90LXhtbA=="}}
	req := httptest.NewRequest(http.MethodPost, "/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	sp := &saml.ServiceProvider{}
	if _, err := parseSAMLResponse(sp, req, "id-1"); err == nil {
		t.Fatal("expected an error for a malformed response")
	}
}
