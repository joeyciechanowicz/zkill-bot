// Package defaults is a blank-import entry point that registers every
// built-in source and action. Binaries that want the standard set import it
// with `_ "github.com/joeyciechanowicz/eve-bot/bot/defaults"`; binaries that
// want a trimmed-down set can cherry-pick the source/action packages instead.
package defaults

import (
	// Actions register via their own init()s on import of the action package
	// (console, webhook, store, reply). Importing bot/run.go already pulls
	// that in, but be explicit here for binaries that only use defaults.
	_ "github.com/joeyciechanowicz/eve-bot/action"

	// Sources register via their own init()s.
	_ "github.com/joeyciechanowicz/eve-bot/internal/source/evescout"
	_ "github.com/joeyciechanowicz/eve-bot/internal/source/zkill"
)
