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
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.21\n"), 0o644); err != nil { //nolint:gosec // G306: test fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte(code), 0o644); err != nil { //nolint:gosec // G306: test fixture
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
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.21\n"), 0o644) //nolint:gosec // G306: test fixture
	_ = os.WriteFile(filepath.Join(dir, "config.go"), []byte(code), 0o644)                         //nolint:gosec // G306: test fixture
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
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil { //nolint:gosec // G306: test fixture
			t.Fatal(err)
		}
		if output, err := exec.Command(python, "-m", "py_compile", path).CombinedOutput(); err != nil {
			t.Fatalf("generated Python does not parse: %v\n%s\n%s", err, output, code)
		}
	}
}

func TestGenerateRust(t *testing.T) {
	code, err := generateRust(sampleSchema(), "AppConfig")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pub struct AppConfig",
		"pub database_url: String",
		"pub port: Option<i64>",
		"pub enabled: bool",
		"pub log_level: Option<LogLevel>",
		"pub feature_flags: Option<String>",
		"pub api_key: Option<String>",
		"pub enum LogLevel",
		"fn parse(value: &str) -> Option<Self>",
		"pub fn load_from(environ: &[(String, String)], env_type: &str) -> Result<AppConfig, ConfigError>",
		`ConfigError::new("PORT", "must be an integer")`,
		`"production" => &["DATABASE_URL", "ENABLED"]`,
		"pub struct ConfigError",
		"fn is_absolute_url",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	checkRustCompiles(t, code)
}

func TestGenerateRustStringOnlyCompiles(t *testing.T) {
	code, err := generateRust([]schemaJSON{{Key: "NAME", Type: "string"}}, "Config")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "is_absolute_url") || strings.Contains(code, "pub enum") {
		t.Fatalf("string-only schema generated extra code:\n%s", code)
	}
	checkRustCompiles(t, code)
}

func TestRustFieldNames(t *testing.T) {
	names := rustFieldNames([]schemaJSON{
		{Key: "DATABASE_URL"}, {Key: "TYPE"}, {Key: "2FA_ENABLED"}, {Key: "a-b"}, {Key: "a_b"},
	})
	want := []string{"database_url", "type_", "_2fa_enabled", "a_b", "a_b_2"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("name %d = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestRustEnumValuesDropDuplicates(t *testing.T) {
	values := rustEnumValues(schemaJSON{Enum: []string{"info", "debug", "info"}})
	if len(values) != 2 || values[0] != "info" || values[1] != "debug" {
		t.Fatalf("values = %#v", values)
	}
	if variants := rustVariantNames([]string{"read-only", "2fa", "read_only"}); variants[0] != "ReadOnly" ||
		variants[1] != "V2fa" || variants[2] != "ReadOnly2" {
		t.Fatalf("variants = %#v", variants)
	}
}

// checkRustCompiles writes the generated file as a crate and runs cargo check.
// It is skipped when cargo is not installed.
func checkRustCompiles(t *testing.T, code string) {
	t.Helper()
	cargo, err := exec.LookPath("cargo")
	if err != nil {
		t.Log("cargo not found; skipping the Rust compile check")
		return
	}
	dir := t.TempDir()
	manifest := "[package]\nname = \"generated\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[lib]\npath = \"config.rs\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(manifest), 0o644); err != nil { //nolint:gosec // G306: test fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.rs"), []byte(code), 0o644); err != nil { //nolint:gosec // G306: test fixture
		t.Fatal(err)
	}
	cmd := exec.Command(cargo, "check", "--quiet", "--offline")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(dir, "target"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Rust does not compile: %v\n%s\n%s", err, output, code)
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
