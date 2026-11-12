# Store conventions

- **Migrations:** goose, one directory per dialect (`migrations/sqlite`,
  `migrations/postgres` added in M3). Never edit an applied migration; add a
  new one.
- **Timestamps:** fixed-width UTC text, `2006-01-02T15:04:05.000000000Z`, so
  lexical order equals chronological order in SQLite. Use the helpers in
  `store.go`.
- **IDs:** application-generated UUIDs (TEXT). No auto-increment keys cross an
  API boundary.
- **Booleans:** `INTEGER` 0/1 on SQLite; keep queries portable.
- **Blobs:** ciphertext only. The store never sees plaintext secret values.
- **Queries:** explicit SQL. `sqlc.yaml` generates `internal/store/sqlcgen` from
  `queries/`. Some audit and job operations remain hand-written because they
  need transaction-scoped logic (hash-chain sealing, atomic lease claims).
- **Postgres:** all queries avoid SQLite-only syntax except the migration
  files. When Postgres is added in M3, add a parallel migration directory and
  keep the same `*Store` method set.
- **Tests:** `storetest.Open(t)` returns a migrated temporary database.
