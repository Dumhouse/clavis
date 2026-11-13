package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitCommandLine(t *testing.T) {
	cases := map[string][]string{
		"bin/rails server":            {"bin/rails", "server"},
		"  npm   run dev ":            {"npm", "run", "dev"},
		`sh -c "make dev && echo ok"`: {"sh", "-c", "make dev && echo ok"},
		`echo 'a "b" c'`:              {"echo", `a "b" c`},
		`echo a\ b`:                   {"echo", "a b"},
		`echo ""`:                     {"echo", ""},
		"":                            nil,
	}
	for in, want := range cases {
		got, err := splitCommandLine(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitCommandLine(%q) = %#v, want %#v", in, got, want)
		}
	}
	for _, bad := range []string{`echo "open`, `echo 'open`, `echo \`} {
		if _, err := splitCommandLine(bad); err == nil {
			t.Errorf("splitCommandLine(%q): expected an error", bad)
		}
	}
}

func TestRunStopsFlagParsingAtCommand(t *testing.T) {
	cases := []struct {
		args    []string
		wantCmd []string
		wantEnv string
	}{
		{[]string{"run", "rails", "s", "-p", "3001"}, []string{"rails", "s", "-p", "3001"}, ""},
		{[]string{"run", "--env", "staging", "npm", "run", "dev", "--", "--host"}, []string{"npm", "run", "dev", "--", "--host"}, "staging"},
		{[]string{"run", "--", "./script", "--verbose"}, []string{"./script", "--verbose"}, ""},
	}
	for _, tc := range cases {
		opts := &Options{}
		root := NewRootCommandWithOptions(opts)
		cmd, rest, err := root.Find(tc.args)
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.ParseFlags(rest); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if got := cmd.Flags().Args(); !reflect.DeepEqual(got, tc.wantCmd) {
			t.Errorf("%v: command = %#v, want %#v", tc.args, got, tc.wantCmd)
		}
		if opts.Env != tc.wantEnv {
			t.Errorf("%v: env = %q, want %q", tc.args, opts.Env, tc.wantEnv)
		}
	}
}

func TestDefaultRunCommand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := defaultRunCommand(); err == nil || !strings.Contains(err.Error(), "run: <command>") {
		t.Fatalf("without config: err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, projectConfigName), []byte("project: web\nrun: bin/rails server -p 3001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "app", "models")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub) // found by walking up, like the rest of the config
	got, err := defaultRunCommand()
	if err != nil || !reflect.DeepEqual(got, []string{"bin/rails", "server", "-p", "3001"}) {
		t.Fatalf("got %#v, err %v", got, err)
	}
}

func TestDetectRunCommand(t *testing.T) {
	cases := []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"package.json": `{"scripts":{"dev":"vite"}}`}, "npm run dev"},
		{map[string]string{"package.json": `{"scripts":{"dev":"next dev"}}`, "pnpm-lock.yaml": ""}, "pnpm run dev"},
		{map[string]string{"package.json": `{"scripts":{"start":"node ."}}`}, "npm start"},
		{map[string]string{"package.json": `{"scripts":{"start":"node ."}}`, "yarn.lock": ""}, "yarn run start"},
		{map[string]string{"Gemfile": "", "bin/rails": ""}, "bin/rails server"},
		{map[string]string{"manage.py": ""}, "python manage.py runserver"},
		{map[string]string{"artisan": ""}, "php artisan serve"},
		{map[string]string{"go.mod": ""}, "go run ."},
		{map[string]string{"Cargo.toml": ""}, "cargo run"},
		{map[string]string{"compose.yaml": ""}, "docker compose up"},
		{map[string]string{"README.md": ""}, ""},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		for name, body := range tc.files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := detectRunCommand(dir); got != tc.want {
			t.Errorf("files %v: got %q, want %q", tc.files, got, tc.want)
		}
	}
}
