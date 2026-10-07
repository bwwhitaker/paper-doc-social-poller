package tiktok

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
)

// Provider adapts Client to the platform.Provider interface.
type Provider struct {
	client *Client
}

func NewProvider(c *Client) *Provider { return &Provider{client: c} }

// Name implements platform.Provider.
func (p *Provider) Name() string { return "tiktok" }

// Refresh implements platform.Provider.
func (p *Provider) Refresh(ctx context.Context, tok platform.Token) (platform.Token, error) {
	return p.client.Refresh(ctx, tok.RefreshToken)
}

// Fetch implements platform.Provider.
func (p *Provider) Fetch(ctx context.Context, tok platform.Token) (platform.Snapshot, error) {
	user, err := p.client.UserInfo(ctx, tok.AccessToken)
	if err != nil {
		return platform.Snapshot{}, fmt.Errorf("user info: %w", err)
	}
	videos, err := p.client.ListVideos(ctx, tok.AccessToken)
	if err != nil {
		return platform.Snapshot{}, fmt.Errorf("list videos: %w", err)
	}

	snap := platform.Snapshot{
		Stats: platform.AccountStats{
			DisplayName: user.DisplayName,
			Followers:   user.FollowerCount,
			Following:   &user.FollowingCount,
			Likes:       &user.LikesCount,
			PostCount:   user.VideoCount,
		},
	}
	for _, v := range videos {
		post := platform.Post{
			ID:              v.ID,
			Title:           strings.TrimSpace(v.Title), // TikTok captions often end with a space
			Description:     v.VideoDescription,
			URL:             v.ShareURL,
			DurationSeconds: v.Duration,
			Views:           v.ViewCount,
			Likes:           v.LikeCount,
			Comments:        v.CommentCount,
			Shares:          v.ShareCount,
		}
		if v.CreateTime != 0 {
			post.PublishedAt = time.Unix(v.CreateTime, 0).UTC()
		}
		snap.Posts = append(snap.Posts, post)
	}
	return snap, nil
}
