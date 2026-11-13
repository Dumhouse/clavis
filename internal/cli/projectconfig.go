package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// projectConfig is the committed .clavis.yaml file. It never contains secrets.
type projectConfig struct {
	Server     string `yaml:"server,omitempty"`
	Org        string `yaml:"org,omitempty"`
	Project    string `yaml:"project,omitempty"`
	Env        string `yaml:"env,omitempty"`
	Path       string `yaml:"path,omitempty"`
	BranchEnvs bool   `yaml:"branch_envs,omitempty"`
	Cache      bool   `yaml:"cache,omitempty"`
	// Run is the default command for a bare `clavis run`, for example
	// "bin/rails server". Quotes group words: `run: 'sh -c "make dev"'`.
	Run string `yaml:"run,omitempty"`
}

const projectConfigName = ".clavis.yaml"

func loadProjectConfig(start string) (*projectConfig, string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, "", err
	}
	for {
		path := filepath.Join(dir, projectConfigName)
		b, err := os.ReadFile(path)
		if err == nil {
			cfg := &projectConfig{}
			if err := yaml.Unmarshal(b, cfg); err != nil {
				return nil, "", fmt.Errorf("cli: parse %s: %w", path, err)
			}
			return cfg, path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("cli: read %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return &projectConfig{}, "", nil
		}
		dir = parent
	}
}

func writeProjectConfig(dir string, cfg *projectConfig) (string, error) {
	path := filepath.Join(dir, projectConfigName)
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil { //nolint:gosec // G306: .clavis.yaml holds no secrets and is meant to be committed
		return "", fmt.Errorf("cli: write %s: %w", path, err)
	}
	return path, nil
}
