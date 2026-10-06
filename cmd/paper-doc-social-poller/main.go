// Command paper-doc-social-poller takes one TikTok snapshot and exits.
// It is meant to be run on a schedule (see .github/workflows/poll.yml).
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/config"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/poller"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/store"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/tiktok"
)

func main() {
	// main can't return an error, so delegate to run() and exit non-zero on
	// failure. A non-zero exit is what makes GitHub Actions mark the job red.
	if err := run(); err != nil {
		slog.Error("poll failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	dryRun := flag.Bool("dry-run", false, "fetch and print stats without writing a snapshot")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// A context carries cancellation and deadlines through every call.
	// This one is cancelled on Ctrl-C or after 2 minutes, whichever is first.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close(context.Background())

	tt := tiktok.NewClient(cfg.TikTokClientKey, cfg.TikTokClientSecret)
	return poller.Run(ctx, tt, db, *dryRun)
}
