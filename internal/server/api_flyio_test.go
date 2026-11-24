package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeFlyio serves the two Machines API endpoints a sync uses and keeps the
// secrets in memory.
func fakeFlyio(t *testing.T, token, app string) (*httptest.Server, func() map[string]string) {
	t.Helper()
	var mu sync.Mutex
	remote := map[string]string{"STALE_SECRET": "old"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		if r.URL.Path != "/v1/apps/"+app+"/secrets" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			names := make([]string, 0, len(remote))
			for name := range remote {
				names = append(names, name)
			}
			sort.Strings(names)
			secrets := make([]map[string]string, 0, len(names))
			for _, name := range names {
				secrets = append(secrets, map[string]string{"name": name, "digest": "digest"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": secrets})
		case http.MethodPost:
			var body struct {
				Values map[string]*string `json:"values"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for name, value := range body.Values {
				if value == nil {
					delete(remote, name)
					continue
				}
				remote[name] = *value
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 2})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]string {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]string, len(remote))
		for name, value := range remote {
			out[name] = value
		}
		return out
	}
}

func TestFlyioSync(t *testing.T) {
	srv, remote := fakeFlyio(t, "fly-token", "my-app")
	f := newFixture(t)
	owner, _ := f.setup(t)
	const syncs = "/api/v1/orgs/acme/projects/web-app/syncs"
	const dev = "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"

	bad := owner.request(http.MethodPost, syncs, map[string]any{
		"environment": "dev", "provider": "flyio", "base_url": srv.URL,
		"flyio_app": "My App", "token": "fly-token",
	})
	if bad.Code != http.StatusUnprocessableEntity && bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid app = %d body=%s", bad.Code, bad.Body)
	}

	created := owner.request(http.MethodPost, syncs, map[string]any{
		"environment": "dev", "provider": "flyio", "base_url": srv.URL,
		"flyio_app": "my-app", "token": "fly-token", "delete_missing": true,
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create flyio sync = %d body=%s", created.Code, created.Body)
	}
	if strings.Contains(created.Body.String(), "fly-token") {
		t.Fatal("sync response leaked the Fly.io token")
	}
	syncBody := decode[struct {
		ID string `json:"id"`
	}](t, created)

	for key, value := range map[string]string{"SECRET_KEY": "super-secret", "VAR_KEY": "config-value"} {
		if rec := owner.request(http.MethodPost, dev, map[string]any{"key": key, "value": value}); rec.Code != http.StatusCreated {
			t.Fatalf("secret %s = %d body=%s", key, rec.Code, rec.Body)
		}
	}

	drift := owner.request(http.MethodPost, syncs+"/"+syncBody.ID+"/drift", nil)
	if drift.Code != http.StatusOK || !strings.Contains(drift.Body.String(), `"supported":true`) ||
		!strings.Contains(drift.Body.String(), "SECRET_KEY") || !strings.Contains(drift.Body.String(), "VAR_KEY") {
		t.Fatalf("drift before run = %d body=%s", drift.Code, drift.Body)
	}

	run := owner.request(http.MethodPost, syncs+"/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	if strings.Contains(run.Body.String(), "super-secret") {
		t.Fatal("run response leaked a secret value")
	}
	got := remote()
	if got["SECRET_KEY"] != "super-secret" || got["VAR_KEY"] != "config-value" {
		t.Fatalf("remote secrets = %v", got)
	}
	if _, ok := got["STALE_SECRET"]; ok {
		t.Fatal("delete_missing kept STALE_SECRET")
	}

	drift = owner.request(http.MethodPost, syncs+"/"+syncBody.ID+"/drift", nil)
	if drift.Code != http.StatusOK || !strings.Contains(drift.Body.String(), `"keys":[]`) {
		t.Fatalf("drift after run = %d body=%s", drift.Code, drift.Body)
	}
}

func TestFlyioSyncConnection(t *testing.T) {
	srv, remote := fakeFlyio(t, "fly-token", "my-app")
	f := newFixture(t)
	owner, _ := f.setup(t)
	const connections = "/api/v1/orgs/acme/connections"
	const syncs = "/api/v1/orgs/acme/projects/web-app/syncs"

	created := owner.request(http.MethodPost, connections, map[string]any{
		"provider": "flyio", "name": "Fly prod", "base_url": srv.URL, "token": "fly-token",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create connection = %d body=%s", created.Code, created.Body)
	}
	if strings.Contains(created.Body.String(), "fly-token") {
		t.Fatal("connection response leaked the token")
	}
	conn := decode[connectionResponse](t, created)

	syncRec := owner.request(http.MethodPost, syncs, map[string]any{
		"environment": "dev", "provider": "flyio", "flyio_app": "my-app", "connection_id": conn.ID,
	})
	if syncRec.Code != http.StatusCreated {
		t.Fatalf("create sync with connection = %d body=%s", syncRec.Code, syncRec.Body)
	}
	syncBody := decode[struct {
		ID string `json:"id"`
	}](t, syncRec)

	test := owner.request(http.MethodPost, connections+"/"+conn.ID+"/test", nil)
	if test.Code != http.StatusOK || !strings.Contains(test.Body.String(), `"ok":true`) ||
		!strings.Contains(test.Body.String(), "my-app") {
		t.Fatalf("test connection = %d body=%s", test.Code, test.Body)
	}

	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "FROM_CONNECTION", "value": "value"}); rec.Code != http.StatusCreated {
		t.Fatalf("secret = %d body=%s", rec.Code, rec.Body)
	}
	run := owner.request(http.MethodPost, syncs+"/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	if remote()["FROM_CONNECTION"] != "value" {
		t.Fatalf("remote secrets = %v", remote())
	}
}
