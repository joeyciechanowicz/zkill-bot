package state_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if s.ActionHistory == nil {
		t.Error("ActionHistory: expected non-nil map")
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	path := tempPath(t)
	s, _ := state.Load(path)

	s.LastSequence = 12345
	s.RecordExecution("12345:my-rule:webhook")

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
	if !s2.HasExecuted("12345:my-rule:webhook") {
		t.Error("HasExecuted: expected true for recorded fingerprint")
	}
	if s2.HasExecuted("other:fingerprint") {
		t.Error("HasExecuted: expected false for unrecorded fingerprint")
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	// Verify the file ends up at the expected path (not a temp name).
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

func TestPruneHistory(t *testing.T) {
	path := tempPath(t)
	s, _ := state.Load(path)

	s.RecordExecution("old-entry")
	// Manually backdating requires direct access — we test the prune boundary.
	// Record something and immediately prune with 0 duration (removes everything).
	s.PruneHistory(0)
	if s.HasExecuted("old-entry") {
		t.Error("PruneHistory: expected old-entry to be pruned")
	}
}

func TestPruneHistory_KeepsRecent(t *testing.T) {
	path := tempPath(t)
	s, _ := state.Load(path)

	s.RecordExecution("recent")
	s.PruneHistory(24 * time.Hour) // recent entries should survive
	if !s.HasExecuted("recent") {
		t.Error("PruneHistory: expected recent entry to survive")
	}
}

func TestFingerprint(t *testing.T) {
	fp := state.Fingerprint(134435757, "my-rule", "webhook")
	want := "134435757:my-rule:webhook"
	if fp != want {
		t.Errorf("Fingerprint: got %q, want %q", fp, want)
	}
}
