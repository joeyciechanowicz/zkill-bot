package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPutGet(t *testing.T) {
	s := newStore(t)
	if err := s.Put("scope", "k", map[string]any{"count": 1.0}, 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	v := s.GetAny("scope", "k")
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if m["count"].(float64) != 1 {
		t.Errorf("count: %v", m["count"])
	}
}

func TestInc(t *testing.T) {
	s := newStore(t)
	for range 3 {
		if err := s.Inc("scope", "k", "count", 1, 0); err != nil {
			t.Fatalf("inc: %v", err)
		}
	}
	v := s.GetAny("scope", "k").(map[string]any)
	if v["count"].(float64) != 3 {
		t.Errorf("count: %v, want 3", v["count"])
	}
}

func TestMerge(t *testing.T) {
	s := newStore(t)
	if err := s.Put("scope", "k", map[string]any{"a": 1.0}, 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.Merge("scope", "k", map[string]any{"b": 2.0}, 0); err != nil {
		t.Fatalf("merge: %v", err)
	}
	v := s.GetAny("scope", "k").(map[string]any)
	if v["a"].(float64) != 1 || v["b"].(float64) != 2 {
		t.Errorf("merged: %v", v)
	}
}

func TestTTLExpiry(t *testing.T) {
	s := newStore(t)
	if err := s.Put("scope", "k", "hi", time.Second); err != nil {
		t.Fatalf("put: %v", err)
	}
	if !s.Exists("scope", "k") {
		t.Fatal("fact should exist immediately")
	}
	// Force expiry by writing directly with a past expires_at.
	_, err := s.db.Exec(
		`UPDATE facts SET expires_at = 1 WHERE scope='scope' AND key='k'`,
	)
	if err != nil {
		t.Fatalf("force expiry: %v", err)
	}
	if s.Exists("scope", "k") {
		t.Error("expired fact still visible")
	}
}

func TestRangeCount(t *testing.T) {
	s := newStore(t)
	for _, k := range []string{"a:1", "a:2", "a:3", "b:1"} {
		if err := s.Put("scope", k, 1, 0); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if n := s.RangeCount("scope", "a:"); n != 3 {
		t.Errorf("a: count %d, want 3", n)
	}
	if n := s.RangeCount("scope", "b:"); n != 1 {
		t.Errorf("b: count %d, want 1", n)
	}
}

func TestCheckpoint(t *testing.T) {
	s := newStore(t)
	if _, ok := s.GetCheckpoint("zkill"); ok {
		t.Error("no checkpoint expected")
	}
	if err := s.SetCheckpoint("zkill", "12345"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok := s.GetCheckpoint("zkill")
	if !ok || v != "12345" {
		t.Errorf("got %q ok=%v", v, ok)
	}
}

func TestActionHistory(t *testing.T) {
	s := newStore(t)
	if s.ActionDone("e1", "fp1") {
		t.Error("not recorded yet")
	}
	if err := s.RecordAction("e1", "fp1"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if !s.ActionDone("e1", "fp1") {
		t.Error("should be recorded")
	}
}

func TestJanitorDeletesExpired(t *testing.T) {
	s := newStore(t)
	if err := s.Put("scope", "gone", "x", 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, err := s.db.Exec(
		`UPDATE facts SET expires_at = 1 WHERE scope='scope' AND key='gone'`,
	)
	if err != nil {
		t.Fatalf("force expiry: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go s.RunJanitor(ctx, 10*time.Millisecond, 0)
	time.Sleep(50 * time.Millisecond)
	cancel()

	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM facts WHERE key='gone'`).Scan(&n)
	if n != 0 {
		t.Errorf("expected 0 rows after janitor, got %d", n)
	}
}
