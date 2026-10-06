# paper-doc-social-poller

Records Paper Doc Tutoring's founder's TikTok performance over time and (later) serves a dashboard for it. The admin site in `paper-doc-app` loads the dashboard from this repo.

TikTok's API only returns current totals, so history exists only because a daily run stores a snapshot.

## How it works

A run-once Go binary, scheduled by GitHub Actions (`.github/workflows/poll.yml`, daily):

1. Load the stored OAuth token from Postgres; refresh it if it expires soon (TikTok rotates refresh tokens, so the new one is saved immediately).
2. Fetch account stats (`/v2/user/info/`) and all public videos (`/v2/video/list/`, paged).
3. Write one account snapshot, upsert each video, and add one snapshot row per video, all in one transaction.

Tables are in [migrations/0001_tiktok.sql](migrations/0001_tiktok.sql).

## Layout

- `cmd/paper-doc-social-poller`: the poller entry point
- `cmd/tiktok-auth`: one-time local helper that stores the founder's first tokens
- `internal/config`, `internal/tiktok`, `internal/store`, `internal/poller`

## Setup

1. Create a TikTok for Developers app. Add Login Kit and Display API, scopes `user.info.basic`, `user.info.stats`, `video.list`, and register a redirect URI. Use Sandbox and add the founder as a target user while testing.
2. Run `migrations/0001_tiktok.sql` in the Supabase SQL editor.
3. Authorize once, locally:
   ```sh
   export DATABASE_URL=... TIKTOK_CLIENT_KEY=... TIKTOK_CLIENT_SECRET=... TIKTOK_REDIRECT_URI=...
   go run ./cmd/tiktok-auth
   ```
4. Poll by hand: `go run ./cmd/paper-doc-social-poller` (add `-dry-run` to print stats without saving). On GitHub: Actions → poll-tiktok → Run workflow (or `gh workflow run poll-tiktok.yml -f dry_run=true`).
5. Add `DATABASE_URL` (Supabase **pooler** string, since GitHub Actions is IPv4), `TIKTOK_CLIENT_KEY`, `TIKTOK_CLIENT_SECRET` as repo secrets.

Refresh tokens last about a year; when one expires the poller fails with a message to re-run `tiktok-auth`.

## To do

- Dashboard (followers over time, per-video growth), served from this repo and loaded by the admin site.
- Admin-only read policies, if the dashboard reads through Supabase's API.
- Submit the TikTok app for review; Sandbox isn't meant for permanent use.
- **Future: TikTok trends.** No workable official source found: the Research API excludes commercial users, and Creative Center is a browser tool with no API. Options are paid third-party data providers or scraping (brittle, against TikTok's terms). First decide what "trends" means: TikTok-wide, or what works in the test prep and tutoring niche.
