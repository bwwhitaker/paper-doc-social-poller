// Command paper-doc-social-poller runs one poll from the command line, for
// local testing. In production the same logic runs as a Vercel function
// (api/poll.go), triggered on a schedule by Supabase pg_cron.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"

	"github.com/bwwhitaker/paper-doc-social-poller/app"
)

func main() {
	// main can't return an error, so delegate to run() and exit non-zero on
	// failure.
	if err := run(); err != nil {
		slog.Error("poll failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	dryRun := flag.Bool("dry-run", false, "fetch and print stats without writing snapshots")
	accountsFile := flag.String("accounts", "app/accounts/accounts.json", "path to the list of accounts to poll")
	only := flag.String("account", "", `poll only this account, as "platform:account_id"`)
	flag.Parse()

	// The context is cancelled on Ctrl-C, and it carries that cancellation
	// through every call underneath.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return app.Run(ctx, app.Options{
		DryRun:       *dryRun,
		Only:         *only,
		AccountsFile: *accountsFile,
	})
}
