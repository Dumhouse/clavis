package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sampleSchema() []schemaJSON {
	return []schemaJSON{
		{
			Key: "DATABASE_URL", Type: "url", Kind: "variable",
			Required: map[string]bool{"production": true}, Pattern: "^postgres://",
			Description: "Primary database", Example: "postgres://localhost/app",
		},
		{Key: "PORT", Type: "int", Kind: "variable"},
		{Key: "ENABLED", Type: "bool", Kind: "variable", Required: map[string]bool{"production": true}},
		{Key: "LOG_LEVEL", Type: "enum", Kind: "variable", Enum: []string{"debug", "info", "warn"}},
		{Key: "FEATURE_FLAGS", Type: "json", Kind: "variable"},
		{Key: "API_KEY", Type: "string", Kind: "secret"},
	}
}

func TestGenerateTypeScript(t *testing.T) {
	code, err := generateTypeScript(sampleSchema(), "AppConfig")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"import { z } from 'zod'",
		"export const appConfigSchema = z.object",
		"export type AppConfig = z.infer<typeof appConfigSchema>",
		`"DATABASE_URL": z.string().url().regex(new RegExp("^postgres://"))`,
		`"LOG_LEVEL": z.enum(["debug", "info", "warn"]).optional()`,
		`"ENABLED": booleanString`,
		`"FEATURE_FLAGS": jsonString.optional()`,
		"export function loadAppConfig",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
}

func TestGenerateGoCompiles(t *testing.T) {
	code, err := generateGo(sampleSchema(), "generated", "Config")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"package generated",
		"type Config struct",
		"DatabaseURL",
		"Port",
		"Enabled",
		"FeatureFlags",
		"json.RawMessage",
		"func LoadFrom(environ []string) (Config, error)",
		`required in %s`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go does not compile: %v\n%s\n%s", err, output, code)
	}
}

func TestGenerateGoStringOnlyHasNoUnusedImports(t *testing.T) {
	code, err := generateGo([]schemaJSON{{Key: "NAME", Type: "string"}}, "config", "Config")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, `"fmt"`) {
		t.Fatalf("string-only schema imported fmt:\n%s", code)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.21\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "config.go"), []byte(code), 0o644)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go does not compile: %v\n%s\n%s", err, output, code)
	}
}

func TestGeneratePython(t *testing.T) {
	code, err := generatePython(sampleSchema(), "AppConfig")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"class AppConfig(BaseModel):",
		"database_url: AnyUrl = Field(alias=\"DATABASE_URL\", pattern=\"^postgres://\")",
		"port: int | None = Field(default=None, alias=\"PORT\")",
		"enabled: bool = Field(alias=\"ENABLED\")",
		"log_level: Literal[\"debug\", \"info\", \"warn\"] | None",
		"def load_config(environ: Mapping[str, str] | None = None) -> AppConfig:",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	if python, err := exec.LookPath("python3"); err == nil {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.py")
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(python, "-m", "py_compile", path).CombinedOutput(); err != nil {
			t.Fatalf("generated Python does not parse: %v\n%s\n%s", err, output, code)
		}
	}
}

func TestGoFieldNamesUseInitialisms(t *testing.T) {
	names := goFieldNames([]schemaJSON{
		{Key: "DATABASE_URL"}, {Key: "API_KEY"}, {Key: "2FA_ENABLED"}, {Key: "SAME"}, {Key: "SAME"},
	})
	want := []string{"DatabaseURL", "APIKey", "Key2faEnabled", "Same", "Same2"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("name %d = %q, want %q", i, names[i], want[i])
		}
	}
}
