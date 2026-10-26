package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS blobs (
	namespace TEXT NOT NULL,
	key       TEXT NOT NULL,
	value     BLOB NOT NULL,
	PRIMARY KEY (namespace, key)
) WITHOUT ROWID;
`

type SQLite struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	if path == "" {
		return nil, errors.New("store: empty database path")
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", pragma, err)
		}
	}

	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: create schema: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}

	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

// Get returns the value stored under (namespace, key).
// It returns ErrNotFound (wrapped) when the key does not exist.
func (s *SQLite) Get(ctx context.Context, namespace Namespace, key string) ([]byte, error) {
	var value []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM blobs WHERE namespace = ? AND key = ?`,
		string(namespace), key,
	).Scan(&value)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("store: %s/%s: %w", namespace, key, ErrNotFound)
	case err != nil:
		return nil, fmt.Errorf("store: get %s/%s: %w", namespace, key, err)
	}

	if value == nil {
		value = []byte{}
	}
	return value, nil
}

// Exists reports whether the key exists.
func (s *SQLite) Exists(ctx context.Context, namespace Namespace, key string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM blobs WHERE namespace = ? AND key = ?`,
		string(namespace), key,
	).Scan(&one)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("store: exists %s/%s: %w", namespace, key, err)
	}
	return true, nil
}

// Set inserts or overwrites the value stored under (namespace, key).
func (s *SQLite) Set(ctx context.Context, namespace Namespace, key string, value []byte) error {
	if value == nil {
		value = []byte{}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO blobs (namespace, key, value) VALUES (?, ?, ?)
		ON CONFLICT(namespace, key) DO UPDATE SET value = excluded.value`,
		string(namespace), key, value,
	)
	if err != nil {
		return fmt.Errorf("store: set %s/%s: %w", namespace, key, err)
	}
	return nil
}

// Create inserts the key only if it does not exist yet.
// The returned bool is false when the key already existed.
func (s *SQLite) Create(ctx context.Context, namespace Namespace, key string, value []byte) (bool, error) {
	if value == nil {
		value = []byte{}
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO blobs (namespace, key, value) VALUES (?, ?, ?)
		ON CONFLICT(namespace, key) DO NOTHING`,
		string(namespace), key, value,
	)
	if err != nil {
		return false, fmt.Errorf("store: create %s/%s: %w", namespace, key, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: create %s/%s: %w", namespace, key, err)
	}
	return affected == 1, nil
}
