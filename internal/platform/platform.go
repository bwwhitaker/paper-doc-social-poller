// Package platform defines what every social network integration provides,
// so the poller and the store never need to know about TikTok, Instagram, etc.
//
// In Go, an interface is satisfied implicitly: any type with the right
// methods is a Provider, with no "implements" keyword. Adding Instagram means
// writing a package with a type that has these methods and registering it in
// main; nothing here changes.
package platform

import (
	"context"
	"time"
)

// Account identifies one account to poll. AccountID is the platform's stable
// ID for it (TikTok's open_id, Instagram's user ID), not the @handle.
type Account struct {
	Platform  string `json:"platform"`
	AccountID string `json:"account_id"`
	Label     string `json:"label"` // human-friendly name for the dashboard
}

// Key is "platform:account_id", used for logs and the -account flag.
func (a Account) Key() string { return a.Platform + ":" + a.AccountID }

// Token is a stored credential. Platforms differ in what they use: TikTok has
// separate access and refresh tokens; a platform with one long-lived token
// leaves the refresh fields zero.
type Token struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time // zero means "no separate refresh token"
}

// AccountStats are account-level numbers. Pointers are for values a platform
// may not have: nil is stored as NULL rather than a misleading 0.
type AccountStats struct {
	DisplayName string
	Followers   int64
	Following   *int64
	Likes       *int64
	PostCount   int64
	Extra       map[string]any // platform-specific extras, stored as jsonb
}

// Post is one video/photo/reel: stable fields plus the current counts.
type Post struct {
	ID              string
	Title           string
	Description     string
	URL             string
	DurationSeconds int
	PublishedAt     time.Time // zero if unknown
	Views           int64
	Likes           int64
	Comments        int64
	Shares          int64
	Extra           map[string]any
}

// Snapshot is everything fetched for one account in one poll.
type Snapshot struct {
	Stats AccountStats
	Posts []Post
}

// Provider is one social network.
type Provider interface {
	// Name is the value used in accounts.json's "platform" field.
	Name() string
	// Refresh returns a fresh token. The poller saves it right away.
	Refresh(ctx context.Context, tok Token) (Token, error)
	// Fetch gets current stats and posts for the account the token belongs to.
	Fetch(ctx context.Context, tok Token) (Snapshot, error)
}
