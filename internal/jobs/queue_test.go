package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/jobs"
	"github.com/dumhouse/clavis/internal/store"
	"github.com/dumhouse/clavis/internal/store/storetest"
)

func TestProcessOneSuccess(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	job, err := s.EnqueueJob(ctx, "echo", []byte("hello"), time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}

	q := jobs.New(s, nil, jobs.WithWorkerID("test"), jobs.WithLease(time.Minute))
	var got []byte
	q.Register("echo", func(_ context.Context, payload []byte) error {
		got = append([]byte(nil), payload...)
		return nil
	})

	claimed, err := q.ProcessOne(ctx)
	if err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if string(got) != "hello" {
		t.Fatalf("payload = %q", got)
	}
	finished, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != store.JobDone {
		t.Fatalf("status = %s", finished.Status)
	}

	claimed, err = q.ProcessOne(ctx)
	if err != nil || claimed {
		t.Fatalf("second claim: %v %v", claimed, err)
	}
}

func TestProcessOneRetriesFailure(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	job, _ := s.EnqueueJob(ctx, "flaky", nil, time.Now().Add(-time.Second))

	q := jobs.New(s, nil,
		jobs.WithWorkerID("test"),
		jobs.WithLease(time.Minute),
		jobs.WithBackoff(time.Millisecond, time.Millisecond),
	)
	q.Register("flaky", func(context.Context, []byte) error { return errors.New("nope") })

	claimed, err := q.ProcessOne(ctx)
	if err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.JobPending || got.LastError != "nope" || got.Attempts != 1 {
		t.Fatalf("job = %+v", got)
	}
}

func TestUnknownHandler(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	job, _ := s.EnqueueJob(ctx, "orphan", nil, time.Now().Add(-time.Second))
	q := jobs.New(s, nil, jobs.WithWorkerID("test"))

	claimed, err := q.ProcessOne(ctx)
	if err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	if got.Status != store.JobPending || got.LastError == "" {
		t.Fatalf("job = %+v", got)
	}
}

func TestPanicRecovered(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	job, _ := s.EnqueueJob(ctx, "boom", nil, time.Now().Add(-time.Second))
	q := jobs.New(s, nil, jobs.WithWorkerID("test"))
	q.Register("boom", func(context.Context, []byte) error { panic("secret material") })

	if _, err := q.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	if got.Status != store.JobPending || got.LastError != "jobs: handler panicked" {
		t.Fatalf("job = %+v", got)
	}
}

func TestRegisterPanics(t *testing.T) {
	s := storetest.Open(t)
	q := jobs.New(s, nil)
	handler := func(context.Context, []byte) error { return nil }
	q.Register("k", handler)
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate registration did not panic")
		}
	}()
	q.Register("k", handler)
}

func TestRunStopsOnContext(t *testing.T) {
	s := storetest.Open(t)
	q := jobs.New(s, nil, jobs.WithPollInterval(time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := q.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBackoff(t *testing.T) {
	base := time.Second
	max := 10 * time.Second
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, time.Second},
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second},
		{63, 10 * time.Second},
	}
	for _, tc := range cases {
		if got := jobs.Backoff(tc.attempt, base, max); got != tc.want {
			t.Errorf("Backoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
	if got := jobs.Backoff(99, 0, 0); got != 10*time.Minute {
		t.Errorf("default backoff = %v", got)
	}
}
