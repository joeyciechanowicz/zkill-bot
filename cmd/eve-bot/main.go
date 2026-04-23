// Command eve-bot runs the stock eve-bot with every built-in source and
// action registered. Private repos wanting custom sources should consume
// github.com/joeyciechanowicz/eve-bot/bot directly and ship their own binary.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joeyciechanowicz/eve-bot/bot"
	_ "github.com/joeyciechanowicz/eve-bot/bot/defaults"
)

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bot.Run(ctx, *cfgPath); err != nil {
		slog.Error("eve-bot", "error", err)
		os.Exit(1)
	}
}
