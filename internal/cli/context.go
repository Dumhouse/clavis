package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
}

const defaultServer = "http://localhost:8080"

func (o *Options) resolveContext(requireScope bool) (*resolvedContext, error) {
	cfg, path, err := loadProjectConfig(".")
	if err != nil {
		return nil, err
	}
	r := &resolvedContext{
		Server:     firstNonEmpty(o.ServerURL, os.Getenv("CLAVIS_SERVER"), cfg.Server, defaultServer),
		Org:        firstNonEmpty(o.Org, os.Getenv("CLAVIS_ORG"), cfg.Org),
		Project:    firstNonEmpty(o.Project, os.Getenv("CLAVIS_PROJECT"), cfg.Project),
		Env:        firstNonEmpty(o.Env, os.Getenv("CLAVIS_ENV"), cfg.Env, "dev"),
		Path:       firstNonEmpty(o.Path, cfg.Path, "/"),
		ConfigPath: path,
		Cache:      cfg.Cache,
	}
	r.Server = strings.TrimRight(r.Server, "/")

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
		return nil, fmt.Errorf("not logged in to %s. Run: clavis login --server %s", r.Server, r.Server)
	}
	return client.New(r.Server,
		client.WithToken(r.Token),
		client.WithUserAgent("clavis-cli/"+version.Version),
	)
}

func (r *resolvedContext) newAnonymousClient() (*client.Client, error) {
	return client.New(r.Server, client.WithUserAgent("clavis-cli/"+version.Version))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
