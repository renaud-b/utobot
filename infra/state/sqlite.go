package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const contextKey = "app_context"

type ContextStore interface {
	LoadContext() ([]byte, bool, error)
	SaveContext([]byte) error
	UpdateContext(func([]byte) ([]byte, error)) error
	Close() error
}

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("state_db_path is required")
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create state db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Hour)

	store := &SQLiteStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS bot_state (
	key TEXT PRIMARY KEY,
	value BLOB NOT NULL,
	updated_at TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("initialize state db: %w", err)
	}
	return nil
}

func (s *SQLiteStore) LoadContext() ([]byte, bool, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT value FROM bot_state WHERE key = ?`, contextKey).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load context: %w", err)
	}

	out := append([]byte(nil), raw...)
	return out, true, nil
}

func (s *SQLiteStore) SaveContext(data []byte) error {
	if len(data) == 0 {
		data = []byte("null")
	}

	if !json.Valid(data) {
		return fmt.Errorf("context must be valid JSON")
	}

	_, err := s.db.Exec(`
INSERT INTO bot_state (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
	value = excluded.value,
	updated_at = excluded.updated_at
`, contextKey, append([]byte(nil), data...), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save context: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateContext(fn func([]byte) ([]byte, error)) error {
	if fn == nil {
		return fmt.Errorf("update function is required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin state transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var raw []byte
	err = tx.QueryRow(`SELECT value FROM bot_state WHERE key = ?`, contextKey).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("load context: %w", err)
	}

	updated, err := fn(append([]byte(nil), raw...))
	if err != nil {
		return err
	}
	if len(updated) == 0 {
		updated = []byte("null")
	}
	if !json.Valid(updated) {
		return fmt.Errorf("context must be valid JSON")
	}

	_, err = tx.Exec(`
INSERT INTO bot_state (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
	value = excluded.value,
	updated_at = excluded.updated_at
`, contextKey, append([]byte(nil), updated...), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save context: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit state transaction: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
