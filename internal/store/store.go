// Package store is the shared SQLite-backed persistence layer used by every
// pipeline. It holds three things:
//
//   - facts: a key/value table that actions write and enrichers/rules read
//     (e.g. "has this attacker killed anyone in the last 30d?")
//   - checkpoints: per-source resume state (e.g. zkill sequence id)
//   - actions_history: idempotency log, keyed by (event_id, action_fingerprint)
//
// Facts support an optional TTL; a background janitor removes expired rows.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database and exposes fact / checkpoint / idempotency
// operations. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies the schema.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	// SQLite serializes writes; one connection keeps contention predictable.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS facts (
			scope       TEXT    NOT NULL,
			key         TEXT    NOT NULL,
			value       TEXT    NOT NULL,
			updated_at  INTEGER NOT NULL,
			expires_at  INTEGER NOT NULL,
			PRIMARY KEY (scope, key)
		)`,
		`CREATE INDEX IF NOT EXISTS facts_expires_idx ON facts(expires_at) WHERE expires_at > 0`,
		`CREATE TABLE IF NOT EXISTS checkpoints (
			source     TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS actions_history (
			event_id    TEXT NOT NULL,
			action_fp   TEXT NOT NULL,
			executed_at INTEGER NOT NULL,
			PRIMARY KEY (event_id, action_fp)
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("store: migrate %q: %w", q, err)
		}
	}
	return nil
}

// --- Facts ---

// Get returns the raw JSON bytes for (scope, key) if present and not expired.
// Returns (nil, false) if the fact is missing or expired.
func (s *Store) Get(scope, key string) ([]byte, bool) {
	var raw string
	var expiresAt int64
	err := s.db.QueryRow(
		`SELECT value, expires_at FROM facts WHERE scope = ? AND key = ?`,
		scope, key,
	).Scan(&raw, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		slog.Warn("store: get fact", "scope", scope, "key", key, "error", err)
		return nil, false
	}
	if expiresAt > 0 && expiresAt <= time.Now().Unix() {
		return nil, false
	}
	return []byte(raw), true
}

// GetAny returns the fact at (scope, key) JSON-decoded into a generic any.
// Returns nil if missing or expired. Used by rule expressions.
func (s *Store) GetAny(scope, key string) any {
	raw, ok := s.Get(scope, key)
	if !ok {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

// Exists returns true if a non-expired fact exists at (scope, key).
func (s *Store) Exists(scope, key string) bool {
	_, ok := s.Get(scope, key)
	return ok
}

// Put writes a fact, overwriting any prior value. ttl=0 means never expire.
func (s *Store) Put(scope, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("store: marshal value: %w", err)
	}
	return s.putRaw(scope, key, raw, ttl)
}

func (s *Store) putRaw(scope, key string, raw []byte, ttl time.Duration) error {
	now := time.Now().Unix()
	expires := int64(0)
	if ttl > 0 {
		expires = now + int64(ttl/time.Second)
	}
	_, err := s.db.Exec(
		`INSERT INTO facts(scope,key,value,updated_at,expires_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(scope,key) DO UPDATE SET
		   value=excluded.value,
		   updated_at=excluded.updated_at,
		   expires_at=excluded.expires_at`,
		scope, key, string(raw), now, expires,
	)
	if err != nil {
		return fmt.Errorf("store: put fact: %w", err)
	}
	return nil
}

// Inc atomically increments a numeric field inside the JSON object stored at
// (scope, key). If no fact exists, a new object {field: by} is created. ttl
// only applies on insert; existing TTLs are left alone unless ttl > 0, in
// which case expiry is refreshed.
func (s *Store) Inc(scope, key, field string, by float64, ttl time.Duration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	var raw string
	err = tx.QueryRow(
		`SELECT value FROM facts WHERE scope=? AND key=?`, scope, key,
	).Scan(&raw)

	obj := map[string]any{}
	if err == nil {
		_ = json.Unmarshal([]byte(raw), &obj)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("store: inc read: %w", err)
	}

	cur, _ := obj[field].(float64)
	obj[field] = cur + by

	newRaw, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("store: inc marshal: %w", err)
	}
	now := time.Now().Unix()
	expires := int64(0)
	if ttl > 0 {
		expires = now + int64(ttl/time.Second)
	}
	_, err = tx.Exec(
		`INSERT INTO facts(scope,key,value,updated_at,expires_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(scope,key) DO UPDATE SET
		   value=excluded.value,
		   updated_at=excluded.updated_at,
		   expires_at=CASE WHEN excluded.expires_at > 0 THEN excluded.expires_at ELSE facts.expires_at END`,
		scope, key, string(newRaw), now, expires,
	)
	if err != nil {
		return fmt.Errorf("store: inc write: %w", err)
	}
	return tx.Commit()
}

// Merge shallow-merges delta into the JSON object stored at (scope, key).
// New keys are added, existing keys are overwritten. If no fact exists, delta
// becomes the new value.
func (s *Store) Merge(scope, key string, delta map[string]any, ttl time.Duration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	var raw string
	err = tx.QueryRow(
		`SELECT value FROM facts WHERE scope=? AND key=?`, scope, key,
	).Scan(&raw)

	obj := map[string]any{}
	if err == nil {
		_ = json.Unmarshal([]byte(raw), &obj)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("store: merge read: %w", err)
	}

	maps.Copy(obj, delta)
	newRaw, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("store: merge marshal: %w", err)
	}
	now := time.Now().Unix()
	expires := int64(0)
	if ttl > 0 {
		expires = now + int64(ttl/time.Second)
	}
	_, err = tx.Exec(
		`INSERT INTO facts(scope,key,value,updated_at,expires_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(scope,key) DO UPDATE SET
		   value=excluded.value,
		   updated_at=excluded.updated_at,
		   expires_at=CASE WHEN excluded.expires_at > 0 THEN excluded.expires_at ELSE facts.expires_at END`,
		scope, key, string(newRaw), now, expires,
	)
	if err != nil {
		return fmt.Errorf("store: merge write: %w", err)
	}
	return tx.Commit()
}

// Delete removes a fact. Missing keys are not an error.
func (s *Store) Delete(scope, key string) error {
	_, err := s.db.Exec(`DELETE FROM facts WHERE scope=? AND key=?`, scope, key)
	return err
}

// RangeCount returns the number of non-expired facts in scope whose key starts
// with prefix. Used for "how many X events have we seen for Y" rule checks.
func (s *Store) RangeCount(scope, prefix string) int {
	now := time.Now().Unix()
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM facts
		 WHERE scope = ? AND key LIKE ? || '%'
		   AND (expires_at = 0 OR expires_at > ?)`,
		scope, prefix, now,
	).Scan(&n)
	if err != nil {
		slog.Warn("store: range count", "scope", scope, "prefix", prefix, "error", err)
		return 0
	}
	return n
}

// --- Checkpoints ---

// GetCheckpoint returns the stored resume value for a source, or ("", false).
func (s *Store) GetCheckpoint(source string) (string, bool) {
	var v string
	err := s.db.QueryRow(
		`SELECT value FROM checkpoints WHERE source=?`, source,
	).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		slog.Warn("store: get checkpoint", "source", source, "error", err)
		return "", false
	}
	return v, true
}

// SetCheckpoint writes or replaces the resume value for a source.
func (s *Store) SetCheckpoint(source, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO checkpoints(source,value,updated_at)
		 VALUES(?,?,?)
		 ON CONFLICT(source) DO UPDATE SET
		   value=excluded.value, updated_at=excluded.updated_at`,
		source, value, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("store: set checkpoint: %w", err)
	}
	return nil
}

// --- Action idempotency ---

// ActionDone returns true if (eventID, fingerprint) has already been recorded.
func (s *Store) ActionDone(eventID, fingerprint string) bool {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM actions_history WHERE event_id=? AND action_fp=?`,
		eventID, fingerprint,
	).Scan(&one)
	return err == nil
}

// RecordAction marks (eventID, fingerprint) as executed. Safe to call twice.
func (s *Store) RecordAction(eventID, fingerprint string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO actions_history(event_id,action_fp,executed_at)
		 VALUES(?,?,?)`,
		eventID, fingerprint, time.Now().Unix(),
	)
	return err
}

// --- Janitor ---

// RunJanitor deletes expired facts and old action history on interval. It
// returns when ctx is cancelled. Safe to call once per store.
func (s *Store) RunJanitor(ctx context.Context, interval time.Duration, actionHistoryTTL time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now().Unix()
			if _, err := s.db.Exec(
				`DELETE FROM facts WHERE expires_at > 0 AND expires_at <= ?`, now,
			); err != nil {
				slog.Warn("janitor: delete expired facts", "error", err)
			}
			if actionHistoryTTL > 0 {
				cutoff := now - int64(actionHistoryTTL/time.Second)
				if _, err := s.db.Exec(
					`DELETE FROM actions_history WHERE executed_at < ?`, cutoff,
				); err != nil {
					slog.Warn("janitor: delete old action history", "error", err)
				}
			}
		}
	}
}
