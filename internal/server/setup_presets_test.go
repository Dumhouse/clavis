package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/internal/config"
)

const presetToken = "preset-token-0123456789-abcdef"

func setupBody(email string) map[string]string {
	return map[string]string{
		"token": presetToken, "email": email, "name": "Owner",
		"password": "correct horse battery staple", "org_name": "Acme",
	}
}

func TestSetupPresetTokenAndOwnerEmail(t *testing.T) {
	f := newFixtureConfig(t, func(cfg *config.Config) {
		cfg.Server.Setup.Token = presetToken
		cfg.Server.Setup.OwnerEmail = "owner@example.com"
	})
	raw, err := os.ReadFile(filepath.Join(f.dir, "setup-token"))
	if err != nil || strings.TrimSpace(string(raw)) != presetToken {
		t.Fatalf("token file = %q, err = %v", raw, err)
	}

	c := &apiClient{t: t, h: f.srv.Handler()}
	status := decode[map[string]any](t, c.request(http.MethodGet, "/api/v1/setup/status", nil))
	if status["required"] != true || status["owner_email"] != "owner@example.com" {
		t.Fatalf("status = %v", status)
	}

	if rec := c.request(http.MethodPost, "/api/v1/setup", setupBody("someone@example.com")); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong email: status = %d body=%s", rec.Code, rec.Body)
	}
	if rec := c.request(http.MethodPost, "/api/v1/setup", setupBody("Owner@Example.com")); rec.Code != http.StatusOK {
		t.Fatalf("preset email (any case): status = %d body=%s", rec.Code, rec.Body)
	}

	after := decode[map[string]any](t, c.request(http.MethodGet, "/api/v1/setup/status", nil))
	if after["required"] != false || after["owner_email"] != nil {
		t.Fatalf("owner email leaked after setup: %v", after)
	}
}

func TestSessionCookieFollowsBaseURL(t *testing.T) {
	cases := []struct {
		baseURL    string
		wantName   string
		wantSecure bool
	}{
		{"https://clavis.example.com", "__Host-clavis_session", true},
		{"http://localhost:8080", "__Host-clavis_session", true},
		{"http://192.168.1.20", "clavis_session", false},
	}
	for _, tc := range cases {
		t.Run(tc.baseURL, func(t *testing.T) {
			f := newFixtureConfig(t, func(cfg *config.Config) {
				cfg.Server.Dev = false
				cfg.Server.BaseURL = tc.baseURL
				cfg.Server.Setup.Token = presetToken
			})
			c := &apiClient{t: t, h: f.srv.Handler()}
			rec := c.request(http.MethodPost, "/api/v1/setup", setupBody("owner@example.com"))
			if rec.Code != http.StatusOK {
				t.Fatalf("setup status = %d body=%s", rec.Code, rec.Body)
			}
			var found *http.Cookie
			for _, ck := range rec.Result().Cookies() {
				if strings.HasSuffix(ck.Name, "clavis_session") {
					found = ck
				}
			}
			if found == nil || found.Name != tc.wantName || found.Secure != tc.wantSecure {
				t.Fatalf("session cookie = %+v, want name %s secure %v", found, tc.wantName, tc.wantSecure)
			}
		})
	}
}

func TestSetupOwnerOnlyThenOrgAndProject(t *testing.T) {
	f := newFixtureConfig(t, func(cfg *config.Config) { cfg.Server.Setup.Token = presetToken })
	c := &apiClient{t: t, h: f.srv.Handler()}
	rec := c.request(http.MethodPost, "/api/v1/setup", map[string]any{
		"token": presetToken, "email": "owner@example.com", "name": "Owner",
		"password": "correct horse battery staple", "skip_project": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", rec.Code, rec.Body)
	}
	body := decode[struct {
		Orgs []struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"orgs"`
	}](t, rec)
	if len(body.Orgs) != 1 || body.Orgs[0].Name != "My organization" {
		t.Fatalf("orgs = %+v", body.Orgs)
	}
	slug := body.Orgs[0].Slug
	projects := decode[struct {
		Projects []projectResponse `json:"projects"`
	}](t, c.request(http.MethodGet, "/api/v1/orgs/"+slug+"/projects", nil))
	if len(projects.Projects) != 0 {
		t.Fatalf("expected no projects, got %+v", projects.Projects)
	}
	// The welcome flow renames the org and creates the first project.
	if rec := c.request(http.MethodPatch, "/api/v1/orgs/"+slug, map[string]string{"name": "Dumhouse", "slug": "dumhouse"}); rec.Code != http.StatusOK {
		t.Fatalf("rename org = %d body=%s", rec.Code, rec.Body)
	}
	if rec := c.request(http.MethodPost, "/api/v1/orgs/dumhouse/projects", map[string]string{"name": "API", "slug": "api"}); rec.Code != http.StatusCreated {
		t.Fatalf("create project = %d body=%s", rec.Code, rec.Body)
	}
}
