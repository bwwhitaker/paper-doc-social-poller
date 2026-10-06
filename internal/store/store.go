// Package store reads and writes the poller's Postgres tables.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/tiktok"
)

type Store struct {
	conn *pgx.Conn
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// Supabase's pooler (transaction mode) doesn't support prepared
	// statements, which pgx uses by default. Simple protocol works on both
	// the pooler and a direct connection.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &Store{conn: conn}, nil
}

func (s *Store) Close(ctx context.Context) error { return s.conn.Close(ctx) }

// ErrNoToken means nobody has authorized the app yet.
var ErrNoToken = errors.New("no tiktok token stored; run cmd/tiktok-auth first")

// LoadToken returns the single stored token row.
func (s *Store) LoadToken(ctx context.Context) (tiktok.Token, error) {
	var t tiktok.Token
	err := s.conn.QueryRow(ctx, `
		select open_id, access_token, access_token_expires_at,
		       refresh_token, refresh_token_expires_at
		from tiktok_oauth_tokens
		order by updated_at desc limit 1`).
		Scan(&t.OpenID, &t.AccessToken, &t.AccessTokenExpiresAt,
			&t.RefreshToken, &t.RefreshTokenExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return tiktok.Token{}, ErrNoToken
	}
	return t, err
}

func (s *Store) SaveToken(ctx context.Context, t tiktok.Token) error {
	_, err := s.conn.Exec(ctx, `
		insert into tiktok_oauth_tokens
		  (open_id, access_token, access_token_expires_at,
		   refresh_token, refresh_token_expires_at, updated_at)
		values ($1, $2, $3, $4, $5, now())
		on conflict (open_id) do update set
		  access_token = excluded.access_token,
		  access_token_expires_at = excluded.access_token_expires_at,
		  refresh_token = excluded.refresh_token,
		  refresh_token_expires_at = excluded.refresh_token_expires_at,
		  updated_at = now()`,
		t.OpenID, t.AccessToken, t.AccessTokenExpiresAt,
		t.RefreshToken, t.RefreshTokenExpiresAt)
	return err
}

// SaveSnapshot writes one poll's worth of data atomically: either the account
// row, all video upserts and all video snapshots land, or none do.
func (s *Store) SaveSnapshot(ctx context.Context, at time.Time, user tiktok.UserStats, videos []tiktok.Video) (err error) {
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	// Rollback after a successful Commit is a harmless no-op, so deferring it
	// is the idiomatic way to guarantee cleanup on every error path.
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		insert into tiktok_account_snapshots
		  (captured_at, follower_count, following_count, likes_count, video_count)
		values ($1, $2, $3, $4, $5)`,
		at, user.FollowerCount, user.FollowingCount, user.LikesCount, user.VideoCount); err != nil {
		return fmt.Errorf("insert account snapshot: %w", err)
	}

	for _, v := range videos {
		var published *time.Time // a nil pointer becomes SQL NULL
		if v.CreateTime != 0 {
			p := time.Unix(v.CreateTime, 0).UTC()
			published = &p
		}
		if _, err := tx.Exec(ctx, `
			insert into tiktok_videos
			  (video_id, title, video_description, share_url, duration_seconds,
			   published_at, first_seen_at, last_seen_at)
			values ($1, $2, $3, $4, $5, $6, $7, $7)
			on conflict (video_id) do update set
			  title = excluded.title,
			  video_description = excluded.video_description,
			  share_url = excluded.share_url,
			  duration_seconds = excluded.duration_seconds,
			  last_seen_at = excluded.last_seen_at`,
			v.ID, v.Title, v.VideoDescription, v.ShareURL, v.Duration, published, at); err != nil {
			return fmt.Errorf("upsert video %s: %w", v.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			insert into tiktok_video_snapshots
			  (video_id, captured_at, view_count, like_count, comment_count, share_count)
			values ($1, $2, $3, $4, $5, $6)`,
			v.ID, at, v.ViewCount, v.LikeCount, v.CommentCount, v.ShareCount); err != nil {
			return fmt.Errorf("insert video snapshot %s: %w", v.ID, err)
		}
	}
	return tx.Commit(ctx)
}
