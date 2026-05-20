package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/app"
)

// parseVersionFlag parses CLI flags and returns whether --version was requested.
func parseVersionFlag(args []string) (bool, error) {
	fs := flag.NewFlagSet("player", flag.ContinueOnError)
	versionFlag := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	return *versionFlag, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithSignal(args, nil)
}

// runWithSignal parses flags, loads config, builds the logger, and delegates
// the full application lifecycle to app.RunWithSignal. Keeping flag parsing
// and config loading in main keeps the boundary between CLI concerns and
// application concerns clear. sigCh may be nil (production) or a synthetic
// channel (tests).
func runWithSignal(args []string, sigCh <-chan os.Signal) error {
	showVersion, err := parseVersionFlag(args)
	if err != nil {
		return err
	}
	if showVersion {
		fmt.Println(internal.Version)
		return nil
	}

	cfg, err := internal.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger := app.BuildLogger(cfg.LogLevel)

	return app.RunWithSignal(cfg, logger, sigCh)
}
