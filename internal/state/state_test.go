package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"zkill-bot/internal/state"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state.json")
}

func TestLoad_NewFile(t *testing.T) {
	s, err := state.Load(tempPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.LastSequence != 0 {
		t.Errorf("LastSequence: got %d, want 0", s.LastSequence)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	path := tempPath(t)
	s, _ := state.Load(path)

	s.LastSequence = 12345

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := state.Load(path)
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if s2.LastSequence != 12345 {
		t.Errorf("LastSequence: got %d, want 12345", s2.LastSequence)
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	path := tempPath(t)
	s, _ := state.Load(path)
	s.LastSequence = 99
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file not found at expected path: %v", err)
	}
	// No leftover temp files.
	dir := filepath.Dir(path)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("unexpected file in state dir: %s", e.Name())
		}
	}
}
