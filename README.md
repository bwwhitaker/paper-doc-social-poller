# paper-doc-social-poller

This go poller records Paper Doc Tutoring's social media performance over time. A Go function on Vercel, triggered by Supabase `pg_cron`, stores snapshots in Supabase Postgres. The admin dashboard that charts them lives in `paper-doc-app` (see [HANDOFF-paper-doc-app.md](HANDOFF-paper-doc-app.md)).

TikTok is supported today; the code is built so Instagram (or others) can be added as another platform.

Social APIs only return current totals, so history exists only because snapshots are stored.

## How it works

1. Supabase `pg_cron` calls `POST /api/poll` on the Vercel deployment **every 5 minutes**, with a bearer secret.
2. For each account in [accounts.json](accounts.json), the poller loads the stored OAuth token (refreshing it if it expires soon; TikTok rotates refresh tokens, so the new one is saved immediately), then fetches account stats and all public posts.
3. It always upserts the account and posts (keeping `last_seen_at` current), but only **stores a snapshot when one is due**, based on the post's age (see [internal/schedule](internal/schedule/schedule.go)):

   | Post age      | Snapshot at most every |
   | ------------- | ---------------------- |
   | under 1 hour  | 5 minutes              |
   | 1 to 48 hours | 1 hour                 |
   | over 48 hours | 24 hours               |

   A post seen for the first time is always snapshotted. Account stats (followers) are stored hourly. A minute of slack covers scheduling jitter.

Accounts are independent: if one fails, the others still run, and the request returns 500 at the end.

TikTok can take a while to show a new video in the API, so the first snapshot lands when TikTok exposes it, not when it was posted. `published_at` is still the real post time.

## accounts.json

The list of accounts to poll. Only listed accounts are polled, even if other tokens are stored.

```json
[{ "platform": "tiktok", "account_id": "<open_id>", "label": "Founder" }]
```

`account_id` is the platform's stable ID (TikTok's `open_id`), not the @handle. `social-auth` prints the line to add after you authorize an account. The IDs aren't secrets, so this file is committed. To use a different file per environment, set the `ACCOUNTS_FILE` env var (files matching `accounts*.json` are bundled with the function).

## Environments

Vercel's Production and Preview environments have separate env vars, so each can point at its own Supabase project:

| Variable                                    | Notes                                                                                                |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `DATABASE_URL`                              | Supabase **transaction pooler** URI (port 6543). Serverless functions open many short connections.   |
| `TIKTOK_CLIENT_KEY`, `TIKTOK_CLIENT_SECRET` | Sandbox app for preview; production app once TikTok approves it.                                     |
| `POLL_SECRET`                               | Random string; the endpoint rejects requests without it. The same value goes into the `pg_cron` job. |
| `ACCOUNTS_FILE`                             | Optional; defaults to `accounts.json`.                                                               |

Each Supabase project needs the migration, its own authorized tokens (run `social-auth` once per database), and its own `pg_cron` job pointing at its own deployment URL.

## Database schema

The tables live in `paper-doc-app`'s Supabase migrations, which are the source of truth (SQL is in the handoff file). This repo only reads and writes them, over a direct Postgres connection that bypasses RLS: `social_accounts`, `social_account_snapshots`, `social_posts`, `social_post_snapshots`, `social_oauth_tokens`.

If you change a column the poller uses, update `internal/store/store.go` to match.

## Layout

- `api/poll.go`: the Vercel function (auth check, then `app.Run`)
- `cmd/paper-doc-social-poller`: same poll from the command line, for local testing
- `cmd/social-auth`: one-time local helper that authorizes an account and stores its first tokens
- `app`: shared entry point; `internal/poller`: per-account loop and snapshot planning; `internal/schedule`: cadence rules
- `internal/platform`: the `Provider` interface and shared types; `internal/tiktok`: TikTok client and its `Provider`
- `internal/config`, `internal/store`

## Adding a platform (e.g. Instagram)

1. New package `internal/<platform>` with a client and a type implementing `platform.Provider` (`Name`, `Refresh`, `Fetch`).
2. Add a case in `buildProviders` in `app/app.go`, and a case in `cmd/social-auth/main.go` for its authorization flow.
3. Add its credentials to `internal/config` and the Vercel env vars.
4. Add accounts to `accounts.json`. No schema change is needed; platform-specific extras go in the `metrics` jsonb columns.

Check the platform's API docs first. Instagram likely needs a Meta developer app and a Business or Creator account.

## Local use

Environment variables are read from the shell only; there is no `.env` loading. Never commit credentials: this repo is public.

```sh
export DATABASE_URL=... TIKTOK_CLIENT_KEY=... TIKTOK_CLIENT_SECRET=... TIKTOK_REDIRECT_URI=...
go run ./cmd/social-auth tiktok                      # authorize an account (log in to it in your browser first)
go run ./cmd/paper-doc-social-poller -dry-run        # fetch and print, write nothing
go run ./cmd/paper-doc-social-poller -account tiktok:<open_id>
go test ./...
```

Call a deployment by hand (dry run, nothing written):

```sh
curl -X POST "https://<deployment>/api/poll?dry_run=1" -H "Authorization: Bearer $POLL_SECRET"
```

Refresh tokens last about a year; when one expires, that account fails with a message to re-run `social-auth`.

## To do

- Verify the Vercel setup on a real deployment: Go runtime version, `maxDuration` for your plan, and that `accounts.json` is bundled.
- Submit the TikTok app for review; Sandbox isn't meant for permanent use.
- Instagram provider.
- **Future: TikTok trends.** No workable official source found: the Research API excludes commercial users, and Creative Center is a browser tool with no API. Options are paid third-party data providers or scraping (brittle, against TikTok's terms). First decide what "trends" means: TikTok-wide, or what works in the test prep and tutoring niche.
