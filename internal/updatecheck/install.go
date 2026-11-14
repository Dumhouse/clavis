// Package updatecheck finds out whether a newer Clavis release exists and how
// this copy of Clavis was installed, so it can show the right update command.
package updatecheck

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

// Install describes how this binary got onto the machine.
type Install struct {
	// Method is a stable id: homebrew, scoop, winget, docker, kubernetes, deb,
	// rpm, apk, go-install, source, script or binary.
	Method string `json:"method"`
	// Label is a human description, such as "Homebrew".
	Label string `json:"label"`
	// Command updates it. Empty when there is nothing to run.
	Command string `json:"command,omitempty"`
	// Note adds context, for example that a source build is updated by hand.
	Note string `json:"note,omitempty"`
}

// Env is what Detect looks at. Tests fill it in; CurrentEnv reads the machine.
type Env struct {
	Exe          string   // resolved path of the running binary
	GOOS         string   // runtime.GOOS
	GOARCH       string   // runtime.GOARCH
	Version      string   // the stamped version
	BuildVersion string   // module version from go install, if any
	Home         string   // the user's home directory
	GoBin        string   // $GOBIN or $GOPATH/bin
	Container    bool     // running inside a container
	Kubernetes   bool     // running in a Kubernetes pod
	OSIDs        []string // ID and ID_LIKE from /etc/os-release
	Latest       string   // the version to update to, for download links
}

// CurrentEnv reads the machine this process runs on.
func CurrentEnv() Env {
	e := Env{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		e.Exe = exe
	}
	e.Home, _ = os.UserHomeDir()
	e.GoBin = os.Getenv("GOBIN")
	if e.GoBin == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" && e.Home != "" {
			gopath = filepath.Join(e.Home, "go")
		}
		if gopath != "" {
			e.GoBin = filepath.Join(gopath, "bin")
		}
	}
	e.Kubernetes = os.Getenv("KUBERNETES_SERVICE_HOST") != ""
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			e.Container = true
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		e.BuildVersion = strings.TrimPrefix(info.Main.Version, "v")
	}
	e.OSIDs = osReleaseIDs()
	return e
}

func osReleaseIDs() []string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "ID" && key != "ID_LIKE") {
			continue
		}
		ids = append(ids, strings.Fields(strings.Trim(value, `"'`))...)
	}
	return ids
}

const (
	installScript = "curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/main/scripts/install.sh | bash"
	chartRef      = "oci://ghcr.io/dumhouse/charts/clavis"
)

// Detect works out the install method from the path of the binary and the
// environment it runs in.
func Detect(e Env) Install {
	exe := strings.ReplaceAll(e.Exe, `\`, "/")
	lower := strings.ToLower(exe)

	switch {
	case e.Kubernetes:
		return Install{Method: "kubernetes", Label: "Kubernetes (Helm)",
			Command: "helm upgrade clavis " + chartRef + " --namespace clavis --reuse-values",
			Note:    "Use the namespace and values file you installed with."}
	case e.Container:
		return Install{Method: "docker", Label: "Docker",
			Command: "docker compose pull && docker compose up -d",
			Note:    "Or `docker pull dumhouse/clavis:latest` and recreate the container."}
	case strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/Caskroom/") ||
		strings.HasPrefix(exe, "/opt/homebrew/") || strings.HasPrefix(exe, "/home/linuxbrew/"):
		return Install{Method: "homebrew", Label: "Homebrew", Command: "brew upgrade dumhouse/tap/clavis"}
	case strings.Contains(lower, "/scoop/apps/") || strings.Contains(lower, "/scoop/shims/"):
		return Install{Method: "scoop", Label: "Scoop", Command: "scoop update clavis"}
	case strings.Contains(lower, "/winget/"):
		return Install{Method: "winget", Label: "WinGet", Command: "winget upgrade Dumhouse.Clavis"}
	}

	if e.GOOS == "linux" && (strings.HasPrefix(exe, "/usr/bin/") || strings.HasPrefix(exe, "/usr/sbin/")) {
		if pkg, ok := linuxPackage(e); ok {
			return pkg
		}
	}

	// Built from source: the stamp carries "+dev", or there is none at all.
	if e.Version == "dev" || strings.Contains(e.Version, "+dev") || strings.HasSuffix(e.Version, "-dev") {
		return Install{Method: "source", Label: "Built from source",
			Command: "git pull && make build",
			Note:    "Pull the latest tag in your clone and build again."}
	}
	// go install records the module version but not our stamp.
	if e.BuildVersion != "" && e.GoBin != "" && strings.HasPrefix(exe, filepath.ToSlash(e.GoBin)+"/") {
		return Install{Method: "go-install", Label: "go install",
			Command: "go install github.com/dumhouse/clavis/cmd/clavis@latest"}
	}
	inHomeBin := e.Home != "" && strings.HasPrefix(exe, filepath.ToSlash(e.Home)+"/.local/bin/")
	if inHomeBin || strings.HasPrefix(exe, "/usr/local/bin/") {
		return Install{Method: "script", Label: "Install script or manual download", Command: installScript}
	}
	if e.GOOS == "windows" {
		return Install{Method: "binary", Label: "Downloaded binary",
			Note: "Download the new archive from https://github.com/Dumhouse/clavis/releases/latest, or switch to Scoop: scoop bucket add dumhouse https://github.com/Dumhouse/scoop-bucket && scoop install clavis"}
	}
	return Install{Method: "binary", Label: "Downloaded binary",
		Command: installScript,
		Note:    "Or download the archive for your system from https://github.com/Dumhouse/clavis/releases/latest."}
}

func linuxPackage(e Env) (Install, bool) {
	arch := e.GOARCH
	version := strings.TrimPrefix(e.Latest, "v")
	if version == "" {
		version = "VERSION"
	}
	base := "https://github.com/Dumhouse/clavis/releases/download/v" + version + "/clavis_" + version + "_linux_" + arch
	has := func(ids ...string) bool {
		for _, id := range e.OSIDs {
			for _, want := range ids {
				if id == want {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has("debian", "ubuntu"):
		return Install{Method: "deb", Label: "Debian package (.deb)",
			Command: "curl -fsSLO " + base + ".deb && sudo dpkg -i clavis_" + version + "_linux_" + arch + ".deb",
			Note:    "There is no apt repository yet, so each release is downloaded."}, true
	case has("rhel", "fedora", "centos", "suse", "opensuse"):
		return Install{Method: "rpm", Label: "RPM package (.rpm)",
			Command: "sudo rpm -U " + base + ".rpm",
			Note:    "There is no yum repository yet, so each release is downloaded."}, true
	case has("alpine"):
		return Install{Method: "apk", Label: "Alpine package (.apk)",
			Command: "curl -fsSLO " + base + ".apk && sudo apk add --allow-untrusted clavis_" + version + "_linux_" + arch + ".apk",
			Note:    "There is no apk repository yet, so each release is downloaded."}, true
	}
	return Install{}, false
}
