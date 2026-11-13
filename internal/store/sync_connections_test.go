package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/store"
	"github.com/dumhouse/clavis/internal/store/storetest"
)

func TestSyncConnections(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	org := storetest.CreateOrg(t, s, "acme")
	other := storetest.CreateOrg(t, s, "other")
	project := storetest.CreateProject(t, s, org, "web")
	env := storetest.CreateEnvironment(t, s, project, "dev", "development")

	conn := &store.SyncConnection{OrgID: org, Provider: "forgejo", Name: "Home", BaseURL: "https://git.example.com", Config: `{"token":"Y2lwaGVy"}`}
	if err := s.CreateSyncConnection(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSyncConnection(ctx, &store.SyncConnection{OrgID: org, Provider: "github", Name: "Home"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate name err = %v", err)
	}
	// Names are per org.
	if err := s.CreateSyncConnection(ctx, &store.SyncConnection{OrgID: other, Provider: "forgejo", Name: "Home"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSyncConnection(ctx, other, conn.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-org get err = %v", err)
	}

	got, err := s.GetSyncConnection(ctx, org, conn.ID)
	if err != nil || got.Config != conn.Config || got.LastUsedAt != nil || got.BaseURL != conn.BaseURL {
		t.Fatalf("get = %+v, %v", got, err)
	}
	used := time.Now().UTC().Truncate(time.Second)
	if err := s.TouchSyncConnection(ctx, conn.ID, used); err != nil {
		t.Fatal(err)
	}
	got.Name = "Renamed"
	got.Config = `{"token":"bmV3"}`
	if err := s.UpdateSyncConnection(ctx, got); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSyncConnections(ctx, org)
	if err != nil || len(list) != 1 || list[0].Name != "Renamed" || list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(used) {
		t.Fatalf("list = %+v, %v", list, err)
	}

	sync := &store.Sync{ProjectID: project, EnvironmentID: env, Provider: "forgejo", Config: "{}", ConnectionID: conn.ID}
	if err := s.CreateSync(ctx, sync); err != nil {
		t.Fatal(err)
	}
	inline := &store.Sync{ProjectID: project, EnvironmentID: env, Provider: "forgejo", Config: `{"token":"eA=="}`}
	if err := s.CreateSync(ctx, inline); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetSync(ctx, project, sync.ID)
	if err != nil || loaded.ConnectionID != conn.ID {
		t.Fatalf("sync = %+v, %v", loaded, err)
	}
	usage, err := s.ListSyncConnectionUsage(ctx, org, conn.ID)
	if err != nil || len(usage[conn.ID]) != 1 || usage[conn.ID][0].SyncID != sync.ID ||
		usage[conn.ID][0].ProjectSlug != "web" || usage[conn.ID][0].Environment != "dev" {
		t.Fatalf("usage = %+v, %v", usage, err)
	}

	if err := s.SetSyncConnection(ctx, project, inline.ID, conn.ID, "{}"); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListSyncConnectionUsage(ctx, org, "")
	if err != nil || len(all[conn.ID]) != 2 {
		t.Fatalf("all usage = %+v, %v", all, err)
	}
	if moved, _ := s.GetSync(ctx, project, inline.ID); moved.Config != "{}" || moved.ConnectionID != conn.ID {
		t.Fatalf("moved = %+v", moved)
	}

	if err := s.DeleteSyncConnection(ctx, other, conn.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-org delete err = %v", err)
	}
	if err := s.DeleteSyncConnection(ctx, org, conn.ID); err != nil {
		t.Fatal(err)
	}
}
