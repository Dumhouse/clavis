package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// detectRunCommand guesses the usual dev command for the project in dir, for
// the `run:` default that clavis init writes. It returns "" when unsure.
func detectRunCommand(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("package.json"):
		return nodeRunCommand(dir, exists)
	case exists("bin/rails"):
		return "bin/rails server"
	case exists("manage.py"):
		return "python manage.py runserver"
	case exists("artisan"):
		return "php artisan serve"
	case exists("go.mod"):
		return "go run ."
	case exists("Cargo.toml"):
		return "cargo run"
	case exists("mvnw"):
		return "./mvnw spring-boot:run"
	case exists("gradlew"):
		return "./gradlew bootRun"
	case exists("compose.yaml"), exists("compose.yml"), exists("docker-compose.yml"), exists("docker-compose.yaml"):
		return "docker compose up"
	}
	return ""
}

func nodeRunCommand(dir string, exists func(string) bool) string {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json")) //nolint:gosec // G304: the user's own project file
	if err != nil {
		return ""
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return ""
	}
	manager := "npm"
	switch {
	case exists("pnpm-lock.yaml"):
		manager = "pnpm"
	case exists("yarn.lock"):
		manager = "yarn"
	case exists("bun.lockb"), exists("bun.lock"):
		manager = "bun"
	}
	for _, script := range []string{"dev", "start"} {
		if _, ok := pkg.Scripts[script]; ok {
			if manager == "npm" && script == "start" {
				return "npm start"
			}
			return manager + " run " + script
		}
	}
	return ""
}
