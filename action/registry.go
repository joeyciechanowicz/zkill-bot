package action

import (
	"fmt"
	"net/http"
	"sync"
)

// Deps is the set of shared dependencies handed to every action factory.
// Fields may be added over time; adding fields is not a breaking change.
type Deps struct {
	HTTPClient *http.Client
	FactWriter FactWriter
}

// Factory builds one Handler instance from shared Deps.
type Factory func(deps Deps) Handler

var (
	regMu    sync.RWMutex
	registry = map[string]Factory{}
)

// Register installs a factory under typeName. Call from an init() in the
// action implementation package. Panics on duplicate registration.
func Register(typeName string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[typeName]; dup {
		panic(fmt.Sprintf("action: type %q registered twice", typeName))
	}
	registry[typeName] = f
}

// BuildHandlers materializes every registered handler against deps. Used by the
// bot at startup to wire the Dispatcher.
func BuildHandlers(deps Deps) map[string]Handler {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make(map[string]Handler, len(registry))
	for name, f := range registry {
		out[name] = f(deps)
	}
	return out
}

// Types returns the set of registered action type names.
func Types() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
