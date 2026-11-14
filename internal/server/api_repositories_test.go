package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListSyncRepositories(t *testing.T) {
	forgejo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token fj" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/user/repos":
			if r.URL.Query().Get("page") != "1" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[
				{"full_name":"dawidof/nexo","private":true,"permissions":{"admin":true}},
				{"full_name":"team/readonly","private":false,"permissions":{"admin":false,"push":true}},
				{"full_name":"old/archived","archived":true,"permissions":{"admin":true}}]`))
		case "/api/v1/user/orgs":
			_, _ = w.Write([]byte(`[{"username":"team"}]`))
		case "/api/v1/user":
			_, _ = w.Write([]byte(`{"login":"dawidof"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer forgejo.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	const path = "/api/v1/orgs/acme/projects/web-app/syncs/repositories"

	rec := owner.request(http.MethodPost, path, map[string]any{"provider": "forgejo", "base_url": forgejo.URL, "token": "fj"})
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	list := decode[struct {
		Repositories []struct {
			FullName string `json:"full_name"`
			CanWrite bool   `json:"can_write"`
		} `json:"repositories"`
		Organizations []string `json:"organizations"`
		Identity      string   `json:"identity"`
	}](t, rec)
	if len(list.Repositories) != 2 || list.Repositories[0].FullName != "dawidof/nexo" || !list.Repositories[0].CanWrite ||
		list.Repositories[1].CanWrite || list.Identity != "dawidof" || len(list.Organizations) != 1 {
		t.Fatalf("list = %+v", list)
	}

	// A platform token works without sending the token again.
	conn := decode[connectionResponse](t, owner.request(http.MethodPost, "/api/v1/orgs/acme/connections", map[string]any{
		"provider": "forgejo", "name": "Home", "base_url": forgejo.URL, "token": "fj",
	}))
	if rec := owner.request(http.MethodPost, path, map[string]any{"provider": "forgejo", "connection_id": conn.ID}); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "dawidof/nexo") {
		t.Fatalf("by connection = %d %s", rec.Code, rec.Body)
	}

	if bad := owner.request(http.MethodPost, path, map[string]any{"provider": "forgejo", "base_url": forgejo.URL, "token": "nope"}); bad.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(bad.Body.String(), "rejected the token") {
		t.Fatalf("bad token = %d %s", bad.Code, bad.Body)
	}
	if none := owner.request(http.MethodPost, path, map[string]any{"provider": "forgejo", "base_url": forgejo.URL}); none.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no token = %d %s", none.Code, none.Body)
	}
}

func TestStoreCISecrets(t *testing.T) {
	stored := map[string]string{}
	forgejo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token fj" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		const prefix = "/api/v1/repos/dawidof/nexo/actions/secrets/"
		if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Data string `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		stored[strings.TrimPrefix(r.URL.Path, prefix)] = body.Data
		w.WriteHeader(http.StatusCreated)
	}))
	defer forgejo.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	const path = "/api/v1/orgs/acme/projects/web-app/syncs/ci-secrets"

	rec := owner.request(http.MethodPost, path, map[string]any{
		"provider": "forgejo", "base_url": forgejo.URL, "token": "fj", "repo": "dawidof/nexo",
		"secrets": map[string]string{"CLAVIS_TOKEN_STAGING": "clv_mt_a", "CLAVIS_TOKEN_PRODUCTION": "clv_mt_b"}, //nolint:gosec // G101: test fixture tokens
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "CLAVIS_TOKEN_PRODUCTION") {
		t.Fatalf("store = %d %s", rec.Code, rec.Body)
	}
	if stored["CLAVIS_TOKEN_STAGING"] != "clv_mt_a" || stored["CLAVIS_TOKEN_PRODUCTION"] != "clv_mt_b" {
		t.Fatalf("stored = %v", stored)
	}

	// Only Clavis settings: this is not a way around syncs.
	if bad := owner.request(http.MethodPost, path, map[string]any{
		"provider": "forgejo", "base_url": forgejo.URL, "token": "fj", "repo": "dawidof/nexo",
		"secrets": map[string]string{"DATABASE_URL": "x"},
	}); bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-CLAVIS name = %d %s", bad.Code, bad.Body)
	}
	if bad := owner.request(http.MethodPost, path, map[string]any{
		"provider": "forgejo", "base_url": forgejo.URL, "token": "nope", "repo": "dawidof/nexo",
		"secrets": map[string]string{"CLAVIS_TOKEN": "x"},
	}); bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad token = %d %s", bad.Code, bad.Body)
	}
}
