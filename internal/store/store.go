// Package store reads and writes the poller's Postgres tables.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
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

// ErrNoToken means nobody has authorized this account yet.
var ErrNoToken = errors.New("no token stored for this account; run cmd/social-auth first")

// LoadToken returns the stored token for one account.
func (s *Store) LoadToken(ctx context.Context, a platform.Account) (platform.Token, error) {
	var (
		t                platform.Token
		refresh          *string // nullable columns scan into pointers
		refreshExpiresAt *time.Time
	)
	err := s.conn.QueryRow(ctx, `
		select access_token, access_token_expires_at,
		       refresh_token, refresh_token_expires_at
		from social_oauth_tokens
		where platform = $1 and account_id = $2`,
		a.Platform, a.AccountID).
		Scan(&t.AccessToken, &t.AccessTokenExpiresAt, &refresh, &refreshExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.Token{}, ErrNoToken
	}
	if err != nil {
		return platform.Token{}, err
	}
	if refresh != nil {
		t.RefreshToken = *refresh
	}
	if refreshExpiresAt != nil {
		t.RefreshTokenExpiresAt = *refreshExpiresAt
	}
	return t, nil
}

func (s *Store) SaveToken(ctx context.Context, a platform.Account, t platform.Token) error {
	_, err := s.conn.Exec(ctx, `
		insert into social_oauth_tokens
		  (platform, account_id, access_token, access_token_expires_at,
		   refresh_token, refresh_token_expires_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, now())
		on conflict (platform, account_id) do update set
		  access_token = excluded.access_token,
		  access_token_expires_at = excluded.access_token_expires_at,
		  refresh_token = excluded.refresh_token,
		  refresh_token_expires_at = excluded.refresh_token_expires_at,
		  updated_at = now()`,
		a.Platform, a.AccountID, t.AccessToken, t.AccessTokenExpiresAt,
		nullIfEmpty(t.RefreshToken), nullIfZero(t.RefreshTokenExpiresAt))
	return err
}

// Plan says which snapshot rows to write on this run. Account and post
// records are always upserted (so last_seen_at stays accurate); snapshot rows
// are only added where the schedule says one is due.
type Plan struct {
	Account bool            // store an account snapshot
	Posts   map[string]bool // post IDs to store a snapshot for
}

// LastSnapshots returns when the account and each of its posts were last
// snapshotted. A zero time means never.
func (s *Store) LastSnapshots(ctx context.Context, a platform.Account) (time.Time, map[string]time.Time, error) {
	var last *time.Time // max() over no rows is NULL
	if err := s.conn.QueryRow(ctx, `
		select max(captured_at) from social_account_snapshots
		where platform = $1 and account_id = $2`,
		a.Platform, a.AccountID).Scan(&last); err != nil {
		return time.Time{}, nil, fmt.Errorf("last account snapshot: %w", err)
	}
	var account time.Time
	if last != nil {
		account = *last
	}

	rows, err := s.conn.Query(ctx, `
		select s.post_id, max(s.captured_at)
		from social_post_snapshots s
		join social_posts p on p.platform = s.platform and p.post_id = s.post_id
		where p.platform = $1 and p.account_id = $2
		group by s.post_id`,
		a.Platform, a.AccountID)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("last post snapshots: %w", err)
	}
	defer rows.Close()
	posts := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return time.Time{}, nil, err
		}
		posts[id] = at
	}
	return account, posts, rows.Err()
}

// SaveSnapshot writes one account's poll atomically: either everything in the
// plan lands, or nothing does.
func (s *Store) SaveSnapshot(ctx context.Context, at time.Time, a platform.Account, snap platform.Snapshot, plan Plan) error {
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	// Rollback after a successful Commit is a harmless no-op, so deferring it
	// is the idiomatic way to guarantee cleanup on every error path.
	defer tx.Rollback(ctx)

	st := snap.Stats
	statsExtra, err := jsonb(st.Extra)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		insert into social_accounts (platform, account_id, label, display_name, updated_at)
		values ($1, $2, $3, $4, $5)
		on conflict (platform, account_id) do update set
		  label = excluded.label,
		  display_name = excluded.display_name,
		  updated_at = excluded.updated_at`,
		a.Platform, a.AccountID, a.Label, st.DisplayName, at); err != nil {
		return fmt.Errorf("upsert account: %w", err)
	}

	if plan.Account {
		if _, err := tx.Exec(ctx, `
			insert into social_account_snapshots
			  (platform, account_id, captured_at, follower_count, following_count,
			   likes_count, post_count, metrics)
			values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			a.Platform, a.AccountID, at, st.Followers, st.Following,
			st.Likes, st.PostCount, statsExtra); err != nil {
			return fmt.Errorf("insert account snapshot: %w", err)
		}
	}

	for _, p := range snap.Posts {
		extra, err := jsonb(p.Extra)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			insert into social_posts
			  (platform, post_id, account_id, title, description, url,
			   duration_seconds, published_at, first_seen_at, last_seen_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
			on conflict (platform, post_id) do update set
			  title = excluded.title,
			  description = excluded.description,
			  url = excluded.url,
			  duration_seconds = excluded.duration_seconds,
			  last_seen_at = excluded.last_seen_at`,
			a.Platform, p.ID, a.AccountID, p.Title, p.Description, p.URL,
			p.DurationSeconds, nullIfZero(p.PublishedAt), at); err != nil {
			return fmt.Errorf("upsert post %s: %w", p.ID, err)
		}
		if !plan.Posts[p.ID] {
			continue
		}
		if _, err := tx.Exec(ctx, `
			insert into social_post_snapshots
			  (platform, post_id, captured_at, view_count, like_count,
			   comment_count, share_count, metrics)
			values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			a.Platform, p.ID, at, p.Views, p.Likes, p.Comments, p.Shares, extra); err != nil {
			return fmt.Errorf("insert post snapshot %s: %w", p.ID, err)
		}
	}
	return tx.Commit(ctx)
}

// jsonb encodes extras for a "$n::jsonb" parameter. Passing a string (not
// []byte) matters: the simple protocol would send []byte as bytea.
func jsonb(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

// A nil pointer becomes SQL NULL.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullIfZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
