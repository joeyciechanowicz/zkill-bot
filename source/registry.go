package source

import (
	"fmt"
	"net/http"
	"sync"
)

// Checkpointer is the minimal subset of the store that a source needs to
// persist its resume state. The bot wires the concrete SQLite-backed store in.
type Checkpointer interface {
	GetCheckpoint(source string) (string, bool)
	SetCheckpoint(source, value string) error
}

// Deps is the set of shared dependencies handed to every source factory.
// Fields may be added over time; adding fields is not a breaking change.
type Deps struct {
	HTTPClient   *http.Client
	Checkpointer Checkpointer
}

// Factory builds one configured Source instance. name is the operator-chosen
// instance name (used for checkpoints and event.Source); params is the raw
// driver-specific YAML block.
type Factory func(name string, params map[string]any, deps Deps) (Source, error)

var (
	regMu    sync.RWMutex
	registry = map[string]Factory{}
)

// Register installs a factory under typeName. Call from an init() in the
// source implementation package. Panics on duplicate registration so conflicts
// surface at process start, not at config-load time.
func Register(typeName string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[typeName]; dup {
		panic(fmt.Sprintf("source: type %q registered twice", typeName))
	}
	registry[typeName] = f
}

// Build constructs a Source by type. Returns an error if typeName is unknown.
func Build(typeName, name string, params map[string]any, deps Deps) (Source, error) {
	regMu.RLock()
	f, ok := registry[typeName]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("source: unknown type %q", typeName)
	}
	return f(name, params, deps)
}

// Types returns the set of registered source type names. Useful for diagnostics.
func Types() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
