package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/stdlib"
)

// pgRebindDriver wraps pgx to accept SQLite-style ? placeholders. The service
// keeps one query form across SQLite and Postgres.
type pgRebindDriver struct {
	inner driver.Driver
}

var registerPGOnce sync.Once

func openPostgresDB(dsn string) (*sql.DB, error) {
	registerPGOnce.Do(func() {
		sql.Register("clavis-pgx", &pgRebindDriver{inner: stdlib.GetDefaultDriver()})
	})
	db, err := sql.Open("clavis-pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	return db, nil
}

func (d *pgRebindDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &pgConn{Conn: conn}, nil
}

type pgConn struct {
	driver.Conn
}

func (c *pgConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(rebind(query))
	if err != nil {
		return nil, err
	}
	return &pgStmt{Stmt: stmt, query: query}, nil
}

func (c *pgConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err := preparer.PrepareContext(ctx, rebind(query))
		if err != nil {
			return nil, err
		}
		return &pgStmt{Stmt: stmt, query: query}, nil
	}
	return c.Prepare(query)
}

func (c *pgConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := c.Conn.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, rebind(query), normalizeArgs(query, args))
	}
	return nil, driver.ErrSkip
}

func (c *pgConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, rebind(query), normalizeArgs(query, args))
	}
	return nil, driver.ErrSkip
}

func (c *pgConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return &pgTx{Tx: tx}, nil
}

func (c *pgConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		tx, err := beginner.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &pgTx{Tx: tx}, nil
	}
	return c.Begin()
}

func (c *pgConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *pgConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *pgConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

type pgStmt struct {
	driver.Stmt
	query string
}

func (s *pgStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.Stmt.Exec(normalizeValues(s.query, args))
}

func (s *pgStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.Stmt.Query(normalizeValues(s.query, args))
}

func (s *pgStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := s.Stmt.(driver.StmtExecContext); ok {
		return execer.ExecContext(ctx, normalizeArgs(s.query, args))
	}
	return nil, driver.ErrSkip
}

func (s *pgStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return queryer.QueryContext(ctx, normalizeArgs(s.query, args))
	}
	return nil, driver.ErrSkip
}

type pgTx struct {
	driver.Tx
}

func (t *pgTx) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := t.Tx.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, rebind(query), normalizeArgs(query, args))
	}
	return nil, driver.ErrSkip
}

func (t *pgTx) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := t.Tx.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, rebind(query), normalizeArgs(query, args))
	}
	return nil, driver.ErrSkip
}

func (t *pgTx) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := t.Tx.(driver.ConnPrepareContext); ok {
		stmt, err := preparer.PrepareContext(ctx, rebind(query))
		if err != nil {
			return nil, err
		}
		return &pgStmt{Stmt: stmt, query: query}, nil
	}
	return nil, driver.ErrSkip
}

// rebind converts ? placeholders to $1..$n. It ignores quoted strings and
// comments so values that contain '?' are safe.
func rebind(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	inSingle, inDouble, inLine, inBlock := false, false, false, false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		switch {
		case inLine:
			b.WriteByte(ch)
			if ch == '\n' {
				inLine = false
			}
		case inBlock:
			b.WriteByte(ch)
			if ch == '*' && i+1 < len(query) && query[i+1] == '/' {
				b.WriteByte('/')
				i++
				inBlock = false
			}
		case inSingle:
			b.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					b.WriteByte('\'')
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			b.WriteByte(ch)
			if ch == '"' {
				inDouble = false
			}
		default:
			switch {
			case ch == '-' && i+1 < len(query) && query[i+1] == '-':
				b.WriteString("--")
				i++
				inLine = true
			case ch == '/' && i+1 < len(query) && query[i+1] == '*':
				b.WriteString("/*")
				i++
				inBlock = true
			case ch == '\'':
				b.WriteByte(ch)
				inSingle = true
			case ch == '"':
				b.WriteByte(ch)
				inDouble = true
			case ch == '?':
				n++
				b.WriteByte('$')
				b.WriteString(strconv.Itoa(n))
			default:
				b.WriteByte(ch)
			}
		}
	}
	return b.String()
}

func normalizeArgs(query string, args []driver.NamedValue) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	copy(out, args)
	if strings.Contains(query, "goose_db_version") {
		return out
	}
	for i := range out {
		if value, ok := out[i].Value.(bool); ok {
			if value {
				out[i].Value = int64(1)
			} else {
				out[i].Value = int64(0)
			}
		}
	}
	return out
}

func normalizeValues(query string, args []driver.Value) []driver.Value {
	out := make([]driver.Value, len(args))
	copy(out, args)
	if strings.Contains(query, "goose_db_version") {
		return out
	}
	for i := range out {
		if value, ok := out[i].(bool); ok {
			if value {
				out[i] = int64(1)
			} else {
				out[i] = int64(0)
			}
		}
	}
	return out
}

var _ = fmt.Sprintf
