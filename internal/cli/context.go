package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/version"
	"github.com/dumhouse/clavis/pkg/client"
)

// resolvedContext is the effective server/project context for a command.
type resolvedContext struct {
	Server     string
	Org        string
	Project    string
	Env        string
	Path       string
	Token      string
	ConfigPath string
	Cache      bool
	// ServerFrom says where Server came from, for error messages.
	ServerFrom string
}

const defaultServer = "http://localhost:8080"

func (o *Options) resolveContext(requireScope bool) (*resolvedContext, error) {
	cfg, path, err := loadProjectConfig(".")
	if err != nil {
		return nil, err
	}
	r := &resolvedContext{
		Server:     firstNonEmpty(o.ServerURL, os.Getenv("CLAVIS_SERVER"), cfg.Server, lastServer(), defaultServer),
		Org:        firstNonEmpty(o.Org, os.Getenv("CLAVIS_ORG"), cfg.Org),
		Project:    firstNonEmpty(o.Project, os.Getenv("CLAVIS_PROJECT"), cfg.Project),
		Env:        firstNonEmpty(o.Env, os.Getenv("CLAVIS_ENV"), cfg.Env, "dev"),
		Path:       firstNonEmpty(o.Path, cfg.Path, "/"),
		ConfigPath: path,
		Cache:      cfg.Cache,
	}
	r.Server = strings.TrimRight(r.Server, "/")
	switch {
	case strings.TrimSpace(o.ServerURL) != "":
		r.ServerFrom = "--server"
	case strings.TrimSpace(os.Getenv("CLAVIS_SERVER")) != "":
		r.ServerFrom = "CLAVIS_SERVER"
	case strings.TrimSpace(cfg.Server) != "":
		r.ServerFrom = path
	case lastServer() != "":
		r.ServerFrom = "your last login"
	default:
		r.ServerFrom = "the default"
	}

	if token := os.Getenv("CLAVIS_TOKEN"); token != "" {
		r.Token = token
	} else if stored, err := credentialStore().Get(r.Server); err == nil {
		r.Token = stored
	} else if !errors.Is(err, ErrNoCredential) {
		return nil, err
	}

	if requireScope {
		if r.Org == "" || r.Project == "" || r.Env == "" {
			return nil, fmt.Errorf("no project context: run `clavis init` or pass --org, --project, and --env")
		}
	}
	return r, nil
}

func (r *resolvedContext) newClient() (*client.Client, error) {
	if r.Token == "" {
		return nil, r.notLoggedIn()
	}
	hc, err := httpClientFor(r.Server, 30*time.Second)
	if err != nil {
		return nil, err
	}
	return client.New(r.Server,
		client.WithToken(r.Token),
		client.WithUserAgent("clavis-cli/"+version.Version),
		client.WithHTTPClient(hc),
	)
}

// notLoggedIn explains which server was asked and why, and points at the
// server you did sign in to when that is a different one.
func (r *resolvedContext) notLoggedIn() error {
	msg := fmt.Sprintf("not logged in to %s (from %s). Run: clavis login --server %s", r.Server, r.ServerFrom, r.Server)
	if last := strings.TrimRight(lastServer(), "/"); last != "" && last != r.Server {
		if _, err := credentialStore().Get(last); err == nil {
			msg += fmt.Sprintf("\nYou are signed in to %s. Use --server %s, set CLAVIS_SERVER=%s, or run `clavis init` to save it in .clavis.yaml.", last, last, last)
		}
	}
	return errors.New(msg)
}

func (r *resolvedContext) newAnonymousClient() (*client.Client, error) {
	hc, err := httpClientFor(r.Server, 30*time.Second)
	if err != nil {
		return nil, err
	}
	return client.New(r.Server, client.WithUserAgent("clavis-cli/"+version.Version), client.WithHTTPClient(hc))
}

// lastServerPath is where `clavis login` remembers the server it signed in
// to, so commands in a directory without .clavis.yaml (like `clavis init`)
// use it instead of localhost.
func lastServerPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clavis", "server"), nil
}

func lastServer() string {
	path, err := lastServerPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's own config file
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func rememberServer(server string) error {
	path, err := lastServerPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(server+"\n"), 0o600)
}

// forgetServer drops the remembered server when signing out of it.
func forgetServer(server string) {
	if lastServer() != server {
		return
	}
	if path, err := lastServerPath(); err == nil {
		_ = os.Remove(path)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
