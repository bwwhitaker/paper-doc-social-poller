// Package poller ties the TikTok client and the store together for one run.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/store"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/tiktok"
)

// refreshMargin: refresh if the access token expires within this window.
const refreshMargin = 5 * time.Minute

// Run performs one poll: ensure a valid token, fetch stats, store a snapshot.
//
// With dryRun set, it fetches and prints the stats but writes no snapshot.
// (A refreshed token is still saved, since rotation makes that unavoidable.)
func Run(ctx context.Context, tt *tiktok.Client, db *store.Store, dryRun bool) error {
	tok, err := db.LoadToken(ctx)
	if err != nil {
		return err
	}

	if time.Until(tok.RefreshTokenExpiresAt) <= 0 {
		return fmt.Errorf("refresh token expired at %s; founder must re-authorize with cmd/tiktok-auth",
			tok.RefreshTokenExpiresAt.Format(time.RFC3339))
	}

	if time.Until(tok.AccessTokenExpiresAt) < refreshMargin {
		slog.Info("refreshing access token")
		fresh, err := tt.Refresh(ctx, tok.RefreshToken)
		if err != nil {
			return fmt.Errorf("refresh token: %w", err)
		}
		if fresh.OpenID == "" {
			fresh.OpenID = tok.OpenID
		}
		// Save immediately: the refresh token may have rotated, and losing the
		// new one would lock us out even if later steps fail.
		if err := db.SaveToken(ctx, fresh); err != nil {
			return fmt.Errorf("save refreshed token: %w", err)
		}
		tok = fresh
	}

	user, err := tt.UserInfo(ctx, tok.AccessToken)
	if err != nil {
		return fmt.Errorf("user info: %w", err)
	}
	videos, err := tt.ListVideos(ctx, tok.AccessToken)
	if err != nil {
		return fmt.Errorf("list videos: %w", err)
	}

	if dryRun {
		fmt.Printf("account: %+v\n", user)
		for _, v := range videos {
			fmt.Printf("video %s: views=%d likes=%d comments=%d shares=%d %q\n",
				v.ID, v.ViewCount, v.LikeCount, v.CommentCount, v.ShareCount, v.Title)
		}
		slog.Info("dry run, nothing written", "videos", len(videos))
		return nil
	}

	now := time.Now().UTC()
	if err := db.SaveSnapshot(ctx, now, user, videos); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	slog.Info("snapshot saved",
		"followers", user.FollowerCount, "videos", len(videos))
	return nil
}
