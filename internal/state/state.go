package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is the persistent runtime state serialized to a JSON file.
type State struct {
	// LastSequence is the most recently successfully processed sequence ID.
	// On restart, polling resumes at LastSequence+1.
	LastSequence int64 `json:"last_sequence"`

	path string // file path, not serialized
}

// Load reads state from path. If the file does not exist a fresh State is
// returned. Any other error is returned to the caller.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{path: path}, nil
		}
		return nil, fmt.Errorf("state: read %q: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state: parse %q: %w", path, err)
	}
	s.path = path
	return &s, nil
}

// Save atomically writes state to disk using a temp-file + rename pattern.
func (s *State) Save() error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("state: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("state: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("state: close temp file: %w", err)
	}
	// Remove destination first — required on Windows where Rename fails if
	// the target already exists.
	os.Remove(s.path)
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("state: rename temp to %q: %w", s.path, err)
	}
	return nil
}
