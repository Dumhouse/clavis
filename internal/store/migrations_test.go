package store

import (
	"strings"
	"testing"
)

func TestMigrationSetsAreAligned(t *testing.T) {
	sqliteEntries, err := migrations.ReadDir("migrations/sqlite")
	if err != nil {
		t.Fatal(err)
	}
	postgresEntries, err := migrations.ReadDir("migrations/postgres")
	if err != nil {
		t.Fatal(err)
	}
	postgres := map[string]bool{}
	for _, entry := range postgresEntries {
		postgres[entry.Name()] = true
	}
	seen := map[string]bool{}
	for _, entry := range sqliteEntries {
		name := entry.Name()
		version := strings.SplitN(name, "_", 2)[0]
		if seen[version] {
			t.Fatalf("duplicate migration version %s", version)
		}
		seen[version] = true
		if !postgres[name] {
			t.Fatalf("postgres migration missing %s", name)
		}
		raw, err := migrations.ReadFile("migrations/sqlite/" + name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, "-- +goose Up") || !strings.Contains(text, "-- +goose Down") {
			t.Fatalf("%s is missing goose Up/Down markers", name)
		}
	}
	if len(postgres) != len(sqliteEntries) {
		t.Fatalf("postgres migrations = %d, sqlite = %d", len(postgres), len(sqliteEntries))
	}
}
