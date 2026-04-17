// Package storage owns the SQLite persistence layer: opening the database,
// applying embedded goose migrations, and exposing sqlc-generated queries.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"

	// Pure-Go SQLite driver registered under the "sqlite" database/sql driver name.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	sqliteDriver = "sqlite"
	gooseDialect = "sqlite3"
)

// Store owns the SQLite connection and exposes all generated queries.
type Store struct {
	conn *sql.DB
	*Queries
}

// Open connects to the SQLite database at dbPath, verifies reachability,
// applies embedded goose migrations, and returns a ready-to-use Store.
func Open(ctx context.Context, dbPath string) (*Store, error) {
	conn, err := sql.Open(sqliteDriver, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", dbPath, err)
	}
	if err := applyMigrations(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return &Store{conn: conn, Queries: New(conn)}, nil
}

// Close releases the underlying SQLite connection.
func (s *Store) Close() error {
	return s.conn.Close()
}

func applyMigrations(ctx context.Context, conn *sql.DB) error {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("scope migrations FS: %w", err)
	}
	provider, err := goose.NewProvider(gooseDialect, conn, sub)
	if err != nil {
		return fmt.Errorf("new goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
