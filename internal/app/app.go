// Package app is the poll entry point shared by the command-line binary and
// the Vercel function, so both behave identically.
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/config"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/poller"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/store"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/tiktok"
)

// runTimeout caps one poll. The Vercel function's maxDuration should be at
// least this long.
const runTimeout = 2 * time.Minute

type Options struct {
	DryRun       bool   // print stats, write no snapshots
	Only         string // "platform:account_id" to poll just one account
	AccountsFile string // path to the accounts list
}

// Run performs one poll of the configured accounts.
func Run(ctx context.Context, opts Options) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	accounts, err := config.LoadAccounts(opts.AccountsFile)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return fmt.Errorf("%s lists no accounts; add one (see README)", opts.AccountsFile)
	}
	if opts.Only != "" {
		accounts = filter(accounts, opts.Only)
		if len(accounts) == 0 {
			return fmt.Errorf("no account %q in %s", opts.Only, opts.AccountsFile)
		}
	}

	providers, err := buildProviders(cfg, accounts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close(context.Background())

	p := &poller.Poller{DB: db, Providers: providers, DryRun: opts.DryRun}
	return p.Run(ctx, accounts)
}

// buildProviders constructs a Provider for each platform that appears in the
// account list. Adding Instagram means adding a case here.
func buildProviders(cfg config.Config, accounts []platform.Account) (map[string]platform.Provider, error) {
	providers := map[string]platform.Provider{}
	for _, a := range accounts {
		if _, done := providers[a.Platform]; done {
			continue
		}
		switch a.Platform {
		case "tiktok":
			if err := cfg.RequireTikTok(); err != nil {
				return nil, err
			}
			providers["tiktok"] = tiktok.NewProvider(
				tiktok.NewClient(cfg.TikTokClientKey, cfg.TikTokClientSecret))
		default:
			return nil, fmt.Errorf("unsupported platform %q in accounts list", a.Platform)
		}
	}
	return providers, nil
}

func filter(accounts []platform.Account, key string) []platform.Account {
	var out []platform.Account
	for _, a := range accounts {
		if a.Key() == key {
			out = append(out, a)
		}
	}
	return out
}
