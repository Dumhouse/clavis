package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/store"
	"github.com/dumhouse/clavis/internal/store/storetest"
)

func TestMigrateCreatesSchema(t *testing.T) {
	if os.Getenv("CLAVIS_TEST_POSTGRES") != "" {
		t.Skip("SQLite schema introspection")
	}
	s := storetest.Open(t)
	want := []string{
		"users", "sessions", "orgs", "memberships", "roles", "role_bindings",
		"projects", "environments", "folders", "items", "item_versions",
		"audit_events", "jobs",
	}
	rows, err := s.DB().Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	for _, table := range want {
		if !got[table] {
			t.Errorf("missing table %s", table)
		}
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.DB() == nil {
		t.Fatal("nil db")
	}
}

func TestOpenMissingDirectory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "missing", "nested", "clavis.db")
	// SQLite cannot create intermediate directories; Open must fail clearly.
	if _, err := store.Open(ctx, path); err == nil {
		t.Fatal("open in missing directory succeeded")
	}
}

func TestOpenHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Open(ctx, filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Fatal("cancelled open succeeded")
	}
}

func newAuditEvent(orgID, action string) *audit.Event {
	return &audit.Event{
		ID:        uuid.NewString(),
		OrgID:     orgID,
		Actor:     audit.Actor{ID: "user-1", Kind: "user"},
		Action:    action,
		Resource:  audit.Resource{Type: "secret", ID: "item-1"},
		CreatedAt: time.Now().UTC(),
		Details:   audit.Details{KeyBlindIndex: "abc"},
	}
}

func TestAuditChainInSQLite(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	org := storetest.CreateOrg(t, s, "acme")

	for i := 0; i < 3; i++ {
		if err := s.AppendAuditEvent(ctx, newAuditEvent(org, "secret.read")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.VerifyAuditChain(ctx, org); err != nil {
		t.Fatal(err)
	}

	last, err := s.LastAuditEvent(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	if last == nil || last.Seq != 3 {
		t.Fatalf("last = %+v", last)
	}
	events, err := s.ListAuditEvents(ctx, org, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != 2 {
		t.Fatalf("page = %+v", events)
	}
	all, err := s.ListAuditEvents(ctx, org, 0, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %d, %v", len(all), err)
	}

	if got, _ := s.LastAuditEvent(ctx, uuid.NewString()); got != nil {
		t.Fatal("unexpected event for unknown org")
	}
}

func TestAuditTamperDetected(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	org := storetest.CreateOrg(t, s, "acme")
	if err := s.AppendAuditEvent(ctx, newAuditEvent(org, "secret.read")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE audit_events SET action = 'secret.write'`); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyAuditChain(ctx, org); err == nil {
		t.Fatal("tampering not detected")
	}
}

func TestAuditAppendRejectsInvalid(t *testing.T) {
	s := storetest.Open(t)
	ev := newAuditEvent("", "secret.read")
	if err := s.AppendAuditEvent(context.Background(), ev); err == nil {
		t.Fatal("invalid event accepted")
	}
}

func TestJobsLifecycle(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)

	job, err := s.EnqueueJob(ctx, "sync.push", []byte(`{"id":1}`), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.Status != store.JobPending || job.Attempts != 0 {
		t.Fatalf("job = %+v", job)
	}

	claimed, err := s.ClaimJob(ctx, "worker-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != job.ID || claimed.Status != store.JobRunning || claimed.Attempts != 1 {
		t.Fatalf("claimed = %+v", claimed)
	}
	if claimed.LockedUntil == nil {
		t.Fatal("claim has no lease")
	}

	second, err := s.ClaimJob(ctx, "worker-2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second != nil {
		t.Fatalf("job claimed twice: %+v", second)
	}

	if err := s.CompleteJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.JobDone {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestJobsRetryAndExhaust(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)

	job, err := s.EnqueueJob(ctx, "rotate", nil, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimJob(ctx, "w", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.FailJob(ctx, job.ID, "boom", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	if got.Status != store.JobPending || got.LastError != "boom" || got.LockedBy != "" {
		t.Fatalf("job after failure = %+v", got)
	}

	// Simulate an exhausted retry budget.
	if _, err := s.DB().ExecContext(ctx, `UPDATE jobs SET attempts = max_attempts WHERE id = ?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimJob(ctx, "w", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.FailJob(ctx, job.ID, "boom", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetJob(ctx, job.ID)
	if got.Status != store.JobFailed {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestClaimFutureJob(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	if _, err := s.EnqueueJob(ctx, "later", nil, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimJob(ctx, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatalf("future job claimed: %+v", job)
	}
}

func TestReapExpiredJobs(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	if _, err := s.EnqueueJob(ctx, "sync", nil, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimJob(ctx, "dead-worker", time.Second); err != nil {
		t.Fatal(err)
	}
	n, err := s.ReapExpiredJobs(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reaped = %d, want 1", n)
	}
	job, err := s.ClaimJob(ctx, "new-worker", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("reclaimed = %+v, %v", job, err)
	}
	if job.Attempts != 2 {
		t.Fatalf("attempts = %d", job.Attempts)
	}
}

func TestJobErrors(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	if _, err := s.EnqueueJob(ctx, "", nil, time.Now()); err == nil {
		t.Fatal("empty kind accepted")
	}
	if _, err := s.ClaimJob(ctx, "", time.Minute); err == nil {
		t.Fatal("empty worker accepted")
	}
	if err := s.CompleteJob(ctx, uuid.NewString()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("complete: %v", err)
	}
	if err := s.FailJob(ctx, uuid.NewString(), "x", time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fail: %v", err)
	}
	if _, err := s.GetJob(ctx, uuid.NewString()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
}

func TestListJobs(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	if _, err := s.EnqueueJob(ctx, "a", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	job, _ := s.EnqueueJob(ctx, "b", nil, time.Now())
	_, _ = s.ClaimJob(ctx, "w", time.Minute)
	_ = s.CompleteJob(ctx, job.ID)

	all, err := s.ListJobs(ctx, "", 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %d, %v", len(all), err)
	}
	done, err := s.ListJobs(ctx, store.JobDone, 10)
	if err != nil || len(done) != 1 || done[0].ID != job.ID {
		t.Fatalf("done = %+v, %v", done, err)
	}
}
