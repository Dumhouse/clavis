package cli

import (
	"testing"
)

func TestLoginServerBecomesDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("CLAVIS_SERVER", "")
	t.Chdir(t.TempDir()) // no .clavis.yaml here

	opts := &Options{}
	resolved, err := opts.resolveContext(false)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Server != defaultServer {
		t.Fatalf("before login = %s", resolved.Server)
	}

	if err := rememberServer("https://192.168.100.234"); err != nil {
		t.Fatal(err)
	}
	if resolved, _ = opts.resolveContext(false); resolved.Server != "https://192.168.100.234" {
		t.Fatalf("after login = %s", resolved.Server)
	}
	// An explicit server still wins.
	t.Setenv("CLAVIS_SERVER", "https://other.test")
	if resolved, _ = opts.resolveContext(false); resolved.Server != "https://other.test" {
		t.Fatalf("CLAVIS_SERVER = %s", resolved.Server)
	}
	t.Setenv("CLAVIS_SERVER", "")

	forgetServer("https://192.168.100.234")
	if resolved, _ = opts.resolveContext(false); resolved.Server != defaultServer {
		t.Fatalf("after logout = %s", resolved.Server)
	}
}
