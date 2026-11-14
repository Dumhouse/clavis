package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		env     Env
		method  string
		command string
	}{
		{"homebrew cask", Env{Exe: "/opt/homebrew/Caskroom/clavis/0.3.2/clavis", Version: "0.3.2"}, "homebrew", "brew upgrade dumhouse/tap/clavis"},
		{"homebrew linux", Env{Exe: "/home/linuxbrew/.linuxbrew/bin/clavis", Version: "0.3.2"}, "homebrew", "brew upgrade dumhouse/tap/clavis"},
		{"scoop", Env{Exe: `C:\Users\me\scoop\apps\clavis\current\clavis.exe`, GOOS: "windows", Version: "0.3.2"}, "scoop", "scoop update clavis"},
		{"winget", Env{Exe: `C:\Users\me\AppData\Local\Microsoft\WinGet\Packages\Dumhouse.Clavis\clavis.exe`, GOOS: "windows", Version: "0.3.2"}, "winget", "winget upgrade Dumhouse.Clavis"},
		{"docker", Env{Exe: "/clavis", Container: true, Version: "0.3.2"}, "docker", "docker compose pull && docker compose up -d"},
		{"kubernetes beats docker", Env{Exe: "/clavis", Container: true, Kubernetes: true, Version: "0.3.2"}, "kubernetes", ""},
		{"deb", Env{Exe: "/usr/bin/clavis", GOOS: "linux", GOARCH: "amd64", OSIDs: []string{"ubuntu", "debian"}, Version: "0.3.2", Latest: "0.3.3"}, "deb", "dpkg -i clavis_0.3.3_linux_amd64.deb"},
		{"rpm", Env{Exe: "/usr/bin/clavis", GOOS: "linux", GOARCH: "arm64", OSIDs: []string{"fedora"}, Version: "0.3.2", Latest: "0.3.3"}, "rpm", "rpm -U https://github.com/Dumhouse/clavis/releases/download/v0.3.3/clavis_0.3.3_linux_arm64.rpm"},
		{"apk", Env{Exe: "/usr/bin/clavis", GOOS: "linux", GOARCH: "amd64", OSIDs: []string{"alpine"}, Version: "0.3.2", Latest: "0.3.3"}, "apk", "apk add"},
		{"source dev stamp", Env{Exe: "/Users/me/src/clavis/bin/clavis", Version: "0.3.2+dev.752e7e5.dirty"}, "source", "git pull && make build"},
		{"source plain dev", Env{Exe: "/tmp/clavis", Version: "dev"}, "source", "make build"},
		{"go install", Env{Exe: "/Users/me/go/bin/clavis", GoBin: "/Users/me/go/bin", BuildVersion: "0.3.2", Version: "0.3.2"}, "go-install", "go install github.com/dumhouse/clavis/cmd/clavis@latest"},
		{"install script", Env{Exe: "/Users/me/.local/bin/clavis", Home: "/Users/me", Version: "0.3.2"}, "script", "install.sh"},
		{"usr local", Env{Exe: "/usr/local/bin/clavis", Version: "0.3.2"}, "script", "install.sh"},
		{"unknown", Env{Exe: "/opt/tools/clavis", Version: "0.3.2"}, "binary", "install.sh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.env)
			if got.Method != tc.method {
				t.Fatalf("method = %q, want %q (%+v)", got.Method, tc.method, got)
			}
			if !strings.Contains(got.Command, tc.command) {
				t.Fatalf("command = %q, want it to contain %q", got.Command, tc.command)
			}
		})
	}
}

func TestNewerAndDrifted(t *testing.T) {
	newer := []struct {
		latest, current string
		want            bool
	}{
		{"0.3.3", "0.3.2", true},
		{"0.3.2", "0.3.2", false},
		{"0.3.2", "0.3.3", false},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.3.3", "0.3.2+dev.abc.dirty", true},
		{"0.3.2", "0.3.2+dev.abc", false},
		{"0.3.3", "dev", false},
		{"nonsense", "0.3.2", false},
	}
	for _, tc := range newer {
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
	if Drifted("0.3.2", "0.3.9") || !Drifted("0.3.2", "0.4.0") || !Drifted("1.0.0", "0.9.0") || Drifted("dev", "0.3.0") {
		t.Fatal("Drifted compares major and minor only")
	}
}

func TestLatest(t *testing.T) {
	serve := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}
	ok := serve(200, `{"tag_name":"v0.4.1"}`)
	defer ok.Close()
	if got, err := Latest(context.Background(), nil, ok.URL); err != nil || got != "0.4.1" {
		t.Fatalf("Latest = %q, %v", got, err)
	}
	for name, body := range map[string]string{
		"prerelease": `{"tag_name":"v0.5.0-rc1","prerelease":true}`,
		"not semver": `{"tag_name":"nightly"}`,
		"bad json":   `<html>`,
	} {
		srv := serve(200, body)
		if got, err := Latest(context.Background(), nil, srv.URL); err == nil {
			t.Errorf("%s: want an error, got %q", name, got)
		}
		srv.Close()
	}
	down := serve(403, `{}`)
	defer down.Close()
	if _, err := Latest(context.Background(), nil, down.URL); err == nil {
		t.Fatal("a rate-limited response must be an error")
	}
}
