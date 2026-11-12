package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func TestNodePublishAndUnpublish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{
			{"key": "DATABASE_URL", "value": "postgres://db", "kind": "variable"},
			{"key": "API_KEY", "value": "secret", "kind": "secret"},
		}})
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "mount")
	node := &nodeServer{nodeID: "n1", server: server.URL, token: "token"}
	_, err := node.NodePublishVolume(context.Background(), &csi.NodePublishVolumeRequest{
		TargetPath: target,
		VolumeContext: map[string]string{
			"org": "acme", "project": "web", "environment": "dev",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"DATABASE_URL": "postgres://db", "API_KEY": "secret"} {
		data, err := os.ReadFile(filepath.Join(target, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q %v", name, data, err)
		}
	}
	if _, err := node.NodeUnpublishVolume(context.Background(), &csi.NodeUnpublishVolumeRequest{TargetPath: target}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target still exists: %v", err)
	}
}
