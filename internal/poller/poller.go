// Package poller runs one poll across a list of accounts.
package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/schedule"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/store"
)

// refreshMargin: refresh if the access token expires within this window.
const refreshMargin = 5 * time.Minute

type Poller struct {
	DB        *store.Store
	Providers map[string]platform.Provider // keyed by platform name
	DryRun    bool                         // print stats, write no snapshots
}

// Run polls every account. One account failing doesn't stop the others; all
// failures are joined into the returned error so the job still ends non-zero.
func (p *Poller) Run(ctx context.Context, accounts []platform.Account) error {
	var errs []error
	for _, a := range accounts {
		if err := p.pollOne(ctx, a); err != nil {
			slog.Error("account failed", "account", a.Key(), "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", a.Key(), err))
		}
	}
	return errors.Join(errs...) // nil if errs is empty
}

func (p *Poller) pollOne(ctx context.Context, a platform.Account) error {
	prov, ok := p.Providers[a.Platform]
	if !ok {
		return fmt.Errorf("unknown platform %q", a.Platform)
	}

	tok, err := p.DB.LoadToken(ctx, a)
	if err != nil {
		return err
	}

	if !tok.RefreshTokenExpiresAt.IsZero() && time.Until(tok.RefreshTokenExpiresAt) <= 0 {
		return fmt.Errorf("refresh token expired at %s; re-authorize with cmd/social-auth",
			tok.RefreshTokenExpiresAt.Format(time.RFC3339))
	}

	if time.Until(tok.AccessTokenExpiresAt) < refreshMargin {
		slog.Info("refreshing access token", "account", a.Key())
		fresh, err := prov.Refresh(ctx, tok)
		if err != nil {
			return fmt.Errorf("refresh token: %w", err)
		}
		// Save immediately: the refresh token may have rotated, and losing the
		// new one would lock us out even if later steps fail.
		if err := p.DB.SaveToken(ctx, a, fresh); err != nil {
			return fmt.Errorf("save refreshed token: %w", err)
		}
		tok = fresh
	}

	snap, err := prov.Fetch(ctx, tok)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	lastAccount, lastPosts, err := p.DB.LastSnapshots(ctx, a)
	if err != nil {
		return err
	}
	plan := buildPlan(now, snap, lastAccount, lastPosts)

	if p.DryRun {
		fmt.Printf("[%s] %s: followers=%d posts=%d (account snapshot due: %v)\n",
			a.Key(), snap.Stats.DisplayName, snap.Stats.Followers, snap.Stats.PostCount, plan.Account)
		for _, post := range snap.Posts {
			fmt.Printf("  post %s: views=%d likes=%d comments=%d shares=%d snapshot due=%v %q\n",
				post.ID, post.Views, post.Likes, post.Comments, post.Shares, plan.Posts[post.ID], post.Title)
		}
		slog.Info("dry run, nothing written", "account", a.Key(), "posts", len(snap.Posts))
		return nil
	}

	if err := p.DB.SaveSnapshot(ctx, now, a, snap, plan); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	slog.Info("poll saved", "account", a.Key(), "followers", snap.Stats.Followers,
		"posts", len(snap.Posts), "post_snapshots", len(plan.Posts), "account_snapshot", plan.Account)
	return nil
}

// buildPlan decides which snapshots to store. It's a pure function (no
// database, no clock of its own), which makes it easy to test.
func buildPlan(now time.Time, snap platform.Snapshot, lastAccount time.Time, lastPosts map[string]time.Time) store.Plan {
	plan := store.Plan{
		Account: lastAccount.IsZero() || schedule.Due(now.Sub(lastAccount), schedule.AccountEvery),
		Posts:   map[string]bool{},
	}
	for _, post := range snap.Posts {
		last, seen := lastPosts[post.ID]
		if !seen {
			plan.Posts[post.ID] = true // first sighting: always snapshot
			continue
		}
		// Unknown publish time counts as old, so it gets the slowest cadence.
		age := 365 * 24 * time.Hour
		if !post.PublishedAt.IsZero() {
			age = max(now.Sub(post.PublishedAt), 0)
		}
		if schedule.Due(now.Sub(last), schedule.PostEvery(age)) {
			plan.Posts[post.ID] = true
		}
	}
	return plan
}
