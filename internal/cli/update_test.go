package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/updatecheck"
)

func TestUpdateNoticePolicy(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	brew := updatecheck.Install{Method: "homebrew", Command: "brew upgrade dumhouse/tap/clavis"}

	lines := updateNotice(updateState{Latest: "0.3.3"}, now, "0.3.2", brew)
	if len(lines) != 2 || !strings.Contains(lines[0], "0.3.3 is available") ||
		!strings.Contains(lines[0], "brew upgrade dumhouse/tap/clavis") ||
		!strings.Contains(lines[1], "CLAVIS_NO_UPDATE_CHECK") {
		t.Fatalf("lines = %q", lines)
	}
	// Shown recently: quiet until a few days pass.
	recent := updateState{Latest: "0.3.3", NotifiedAt: now.Add(-24 * time.Hour)}
	if got := updateNotice(recent, now, "0.3.2", brew); got != nil {
		t.Fatalf("repeated too soon: %q", got)
	}
	recent.NotifiedAt = now.Add(-4 * 24 * time.Hour)
	if got := updateNotice(recent, now, "0.3.2", brew); got == nil {
		t.Fatal("should repeat after a few days")
	}
	// Up to date and same minor as the server: nothing.
	if got := updateNotice(updateState{Latest: "0.3.2", Server: "https://c.test", ServerVersion: "0.3.5"}, now, "0.3.2", brew); got != nil {
		t.Fatalf("up to date = %q", got)
	}
	// Server a minor version ahead.
	got := updateNotice(updateState{Latest: "0.4.0", Server: "https://c.test", ServerVersion: "0.4.0"}, now, "0.3.2", brew)
	if len(got) != 3 || !strings.Contains(got[1], "newer than this CLI") {
		t.Fatalf("drift = %q", got)
	}
	// Server behind the CLI.
	got = updateNotice(updateState{Latest: "0.4.0", Server: "https://c.test", ServerVersion: "0.3.1"}, now, "0.4.0", brew)
	if len(got) != 2 || !strings.Contains(got[0], "newer than the server") {
		t.Fatalf("old server = %q", got)
	}
}

func TestRefreshUpdateStateIsRateLimited(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	calls := 0
	result, resultErr := "0.3.3", error(nil)
	restore := fetchLatestRelease
	fetchLatestRelease = func(context.Context) (string, error) {
		calls++
		return result, resultErr
	}
	t.Cleanup(func() { fetchLatestRelease = restore })

	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	opts := &Options{}
	s := refreshUpdateState(context.Background(), opts, updateState{}, now, false)
	if calls != 1 || s.Latest != "0.3.3" {
		t.Fatalf("first check: calls=%d state=%+v", calls, s)
	}
	s = refreshUpdateState(context.Background(), opts, s, now.Add(time.Hour), false)
	if calls != 1 {
		t.Fatal("checked again within a day")
	}
	s = refreshUpdateState(context.Background(), opts, s, now.Add(25*time.Hour), false)
	if calls != 2 {
		t.Fatal("did not check after a day")
	}
	// A failed check (offline) waits an hour before retrying, not a day.
	result, resultErr = "", errors.New("offline")
	failed := refreshUpdateState(context.Background(), opts, updateState{}, now, false)
	if refreshUpdateState(context.Background(), opts, failed, now.Add(10*time.Minute), false); calls != 3 {
		t.Fatalf("retried too soon: calls=%d", calls)
	}
	refreshUpdateState(context.Background(), opts, failed, now.Add(2*time.Hour), false)
	if calls != 4 {
		t.Fatalf("did not retry after an hour: calls=%d", calls)
	}
}

func TestUpdateStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	restore := updateStatePath
	updateStatePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { updateStatePath = restore })
	want := updateState{Latest: "0.3.3", CheckedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	saveUpdateState(want)
	if got := loadUpdateState(); got.Latest != want.Latest || !got.CheckedAt.Equal(want.CheckedAt) {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdateChecksDisabled(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("CLAVIS_NO_UPDATE_CHECK", "")
	if updateChecksDisabled() {
		t.Fatal("enabled by default")
	}
	t.Setenv("CI", "true")
	if !updateChecksDisabled() {
		t.Fatal("CI turns it off")
	}
	t.Setenv("CI", "")
	t.Setenv("CLAVIS_NO_UPDATE_CHECK", "1")
	if !updateChecksDisabled() {
		t.Fatal("opt-out")
	}
}

func TestNotLoggedInPointsAtSignedInServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("CLAVIS_TOKEN", "")
	t.Setenv("CLAVIS_CREDENTIAL_FILE", filepath.Join(home, "credentials.json"))
	t.Setenv("CLAVIS_SERVER", "http://localhost:8080")
	t.Chdir(t.TempDir())

	if err := credentialStore().Set("https://192.168.100.234", "tok"); err != nil {
		t.Fatal(err)
	}
	if err := rememberServer("https://192.168.100.234"); err != nil {
		t.Fatal(err)
	}
	resolved, err := (&Options{}).resolveContext(false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolved.newClient()
	if err == nil {
		t.Fatal("want not logged in")
	}
	msg := err.Error()
	for _, want := range []string{"from CLAVIS_SERVER", "You are signed in to https://192.168.100.234", "--server https://192.168.100.234"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q lacks %q", msg, want)
		}
	}
}
