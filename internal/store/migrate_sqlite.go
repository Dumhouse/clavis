package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// MigrationReport summarises a SQLite to Postgres copy.
type MigrationReport struct {
	Tables map[string]int
	Rows   int
}

// MigrateFromSQLite copies every application table from a SQLite store into an
// already-migrated Postgres store. The destination must be empty unless force
// is set.
func MigrateFromSQLite(ctx context.Context, source, dest *Store, force bool) (*MigrationReport, error) {
	if source.dialect != "sqlite" || dest.dialect != "postgres" {
		return nil, fmt.Errorf("store: migrate-db requires sqlite source and postgres destination")
	}
	tables, err := sqliteTables(ctx, source.db)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("store: source database has no tables")
	}
	ordered, err := orderTablesByForeignKeys(ctx, source.db, tables)
	if err != nil {
		return nil, err
	}
	var existing int
	if err := dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&existing); err != nil {
		return nil, fmt.Errorf("store: destination is not migrated: %w", err)
	}
	if existing > 0 && !force {
		return nil, fmt.Errorf("store: destination is not empty; pass --force to replace it")
	}

	tx, err := dest.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin migrate-db: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if force {
		for i := len(ordered) - 1; i >= 0; i-- {
			if _, err := tx.ExecContext(ctx, `DELETE FROM "`+ordered[i]+`"`); err != nil {
				return nil, fmt.Errorf("store: clear %s: %w", ordered[i], err)
			}
		}
	}

	report := &MigrationReport{Tables: map[string]int{}}
	for _, table := range ordered {
		count, err := copyTable(ctx, source.db, tx, dest.dialect, table)
		if err != nil {
			return nil, err
		}
		report.Tables[table] = count
		report.Rows += count
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit migrate-db: %w", err)
	}
	return report, nil
}

func sqliteTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'goose_db_version'
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: list sqlite tables: %w", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func orderTablesByForeignKeys(ctx context.Context, db *sql.DB, tables []string) ([]string, error) {
	known := make(map[string]bool, len(tables))
	for _, table := range tables {
		known[table] = true
	}
	deps := make(map[string][]string, len(tables))
	for _, table := range tables {
		rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_list("`+table+`")`)
		if err != nil {
			return nil, fmt.Errorf("store: foreign keys for %s: %w", table, err)
		}
		for rows.Next() {
			var (
				id, seq               int
				refTable, from, to    string
				onUpdate, onDelete, m string
			)
			if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &m); err != nil {
				rows.Close()
				return nil, err
			}
			if refTable != table && known[refTable] {
				deps[table] = append(deps[table], refTable)
			}
		}
		rows.Close()
	}
	ordered := make([]string, 0, len(tables))
	state := make(map[string]int, len(tables))
	var visit func(string) error
	visit = func(table string) error {
		switch state[table] {
		case 2:
			return nil
		case 1:
			return nil // cycle; fall back to existing order
		}
		state[table] = 1
		for _, dep := range deps[table] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[table] = 2
		ordered = append(ordered, table)
		return nil
	}
	sorted := append([]string(nil), tables...)
	sort.Strings(sorted)
	for _, table := range sorted {
		if err := visit(table); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func copyTable(ctx context.Context, source *sql.DB, dest *sql.Tx, dialect, table string) (int, error) {
	columns, err := sqliteColumns(ctx, source, table)
	if err != nil {
		return 0, err
	}
	if len(columns) == 0 {
		return 0, nil
	}
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = `"` + column + `"`
	}
	selectSQL := `SELECT ` + strings.Join(quoted, ", ") + ` FROM "` + table + `"`
	rows, err := source.QueryContext(ctx, selectSQL)
	if err != nil {
		return 0, fmt.Errorf("store: read %s: %w", table, err)
	}
	defer rows.Close()

	placeholders := make([]string, len(columns))
	for i := range columns {
		if dialect == "postgres" {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		} else {
			placeholders[i] = "?"
		}
	}
	insertSQL := `INSERT INTO "` + table + `" (` + strings.Join(quoted, ", ") + `) VALUES (` +
		strings.Join(placeholders, ", ") + `)`
	count := 0
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return count, fmt.Errorf("store: scan %s: %w", table, err)
		}
		if _, err := dest.ExecContext(ctx, insertSQL, values...); err != nil {
			return count, fmt.Errorf("store: insert %s row %d: %w", table, count+1, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("store: read %s: %w", table, err)
	}
	return count, nil
}

func sqliteColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("`+table+`")`)
	if err != nil {
		return nil, fmt.Errorf("store: columns for %s: %w", table, err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}
