# Handoff: what to do in paper-doc-app

Context for whoever (you or a Claude session) works in `paper-doc-app`. The poller in `github.com/bwwhitaker/paper-doc-social-poller` (a Go function on Vercel, triggered every 5 minutes by Supabase pg_cron) writes social-media snapshots into Supabase Postgres. This repo's job is to **own the schema** and **show the data** in the admin area.

Written for: whoever implements the schema and dashboard in paper-doc-app.

## 1. Add the migration

Put the SQL below in paper-doc-app's Supabase migrations (e.g. `supabase/migrations/<timestamp>_social_stats.sql`) and run it. The poller already assumes these exact table and column names (`internal/store/store.go` in the poller repo); if you change one, change the poller too.

The tables are platform-generic (`platform` + `account_id` columns) so Instagram can be added later without a schema change. TikTok is the only platform today, with `account_id` = TikTok's `open_id`.

```sql
-- One row per tracked account. Upserted by the poller on every run.
create table social_accounts (
  platform text not null,
  account_id text not null,          -- platform's stable ID (TikTok open_id)
  label text,                        -- friendly name from the poller's accounts.json
  display_name text,                 -- name reported by the platform
  updated_at timestamptz not null default now(),
  primary key (platform, account_id)
);

-- One row per account per poll.
create table social_account_snapshots (
  id bigint generated always as identity primary key,
  platform text not null,
  account_id text not null,
  captured_at timestamptz not null default now(),
  follower_count bigint not null,
  following_count bigint,            -- null if the platform doesn't report it
  likes_count bigint,                -- null if the platform doesn't report it
  post_count integer not null,
  metrics jsonb not null default '{}',  -- platform-specific extras
  foreign key (platform, account_id) references social_accounts (platform, account_id)
);

create index social_account_snapshots_account_time
  on social_account_snapshots (platform, account_id, captured_at);

-- One row per post (video/reel/etc.): the stable fields only.
create table social_posts (
  platform text not null,
  post_id text not null,
  account_id text not null,
  title text,
  description text,
  url text,
  duration_seconds integer,
  published_at timestamptz,
  first_seen_at timestamptz not null default now(),
  last_seen_at timestamptz not null default now(),
  primary key (platform, post_id),
  foreign key (platform, account_id) references social_accounts (platform, account_id)
);

create index social_posts_account on social_posts (platform, account_id);

-- One row per post per poll: only the changing counts.
create table social_post_snapshots (
  id bigint generated always as identity primary key,
  platform text not null,
  post_id text not null,
  captured_at timestamptz not null default now(),
  view_count bigint not null,
  like_count bigint not null,
  comment_count bigint not null,
  share_count bigint not null,
  metrics jsonb not null default '{}',
  foreign key (platform, post_id) references social_posts (platform, post_id)
);

create index social_post_snapshots_post_time
  on social_post_snapshots (platform, post_id, captured_at);

-- OAuth tokens. RLS on with NO policies: only the poller's direct Postgres
-- connection (which bypasses RLS) can read or write this. Never add a policy.
create table social_oauth_tokens (
  platform text not null,
  account_id text not null,
  access_token text not null,
  access_token_expires_at timestamptz not null,
  refresh_token text,                    -- null for platforms with one long-lived token
  refresh_token_expires_at timestamptz,
  updated_at timestamptz not null default now(),
  primary key (platform, account_id)
);

alter table social_accounts enable row level security;
alter table social_account_snapshots enable row level security;
alter table social_posts enable row level security;
alter table social_post_snapshots enable row level security;
alter table social_oauth_tokens enable row level security;

-- Admin read access for the dashboard. REPLACE the condition with however
-- this repo's existing admin policies identify an admin; the profiles/role
-- check below is only a placeholder.
create policy "admins read social_accounts" on social_accounts
  for select using (
    exists (select 1 from profiles where id = auth.uid() and role = 'admin')
  );
create policy "admins read social_account_snapshots" on social_account_snapshots
  for select using (
    exists (select 1 from profiles where id = auth.uid() and role = 'admin')
  );
create policy "admins read social_posts" on social_posts
  for select using (
    exists (select 1 from profiles where id = auth.uid() and role = 'admin')
  );
create policy "admins read social_post_snapshots" on social_post_snapshots
  for select using (
    exists (select 1 from profiles where id = auth.uid() and role = 'admin')
  );
```

Notes:
- Read-only for the app: no insert/update/delete policies. The poller writes with a direct connection.
- The token table is the only sensitive one. Confirm after running that selecting from it as a normal or admin user returns no rows.

## 2. Build `/admin/social/dashboard`

A Next.js App Router page in the existing admin area (use the `AdminShell.jsx` pattern and whatever admin gate the other admin pages use). Query Supabase with the logged-in admin's session; the RLS policies above do the access control.

Suggested contents:
- **Account picker** from `social_accounts` (show `label`, fall back to `display_name`). Default to the first account; filter everything below by `platform` + `account_id`.
- **Followers over time:** line chart from `social_account_snapshots` (`captured_at`, `follower_count`). Optionally likes and post count.
- **Posts table:** one row per post with latest views/likes/comments/shares, and a sparkline or growth number.
- **Per-post growth:** line chart of `view_count` over `captured_at` for a selected post.

Useful queries:

```sql
-- Followers over time for one account
select captured_at, follower_count
from social_account_snapshots
where platform = 'tiktok' and account_id = $1
order by captured_at;

-- Latest counts per post, plus change since the previous poll
select p.post_id, p.title, p.published_at, p.url,
       s.view_count, s.like_count, s.comment_count, s.share_count,
       s.view_count - lag(s.view_count) over (
         partition by s.platform, s.post_id order by s.captured_at) as views_since_last_poll,
       s.captured_at
from social_posts p
join social_post_snapshots s using (platform, post_id)
where p.platform = 'tiktok' and p.account_id = $1
order by s.captured_at desc;  -- take the newest row per post in app code or with distinct on
```

Things to know when charting:
- Snapshot spacing is **uneven by design**: every 5 minutes for a post's first hour, hourly until 48 hours, then daily. Plot against real timestamps (a time axis), not evenly spaced points, and consider downsampling the dense early part for older views.
- Counts can go **down** (a video deleted or hidden, a follower lost). Don't assume monotonic.
- A post that disappears from TikTok stops getting new snapshots but keeps its history; `last_seen_at` tells you when it was last seen.
- `following_count` and `likes_count` are nullable for platforms that don't provide them.
- Tables are empty until the poller has run at least once.

## 3. Schedule the poller with pg_cron

The poller is a Vercel function (separate project, deployed from the poller repo). Supabase calls it every 5 minutes; the function decides which runs actually store a snapshot (hourly for posts under 48 hours old, daily after, and so on; see the poller README). Do this once per Supabase project, pointing at that environment's deployment.

Written from memory of Supabase's `pg_cron`, `pg_net` and Vault, and not run, so check the function signatures against the current docs.

```sql
create extension if not exists pg_cron;
create extension if not exists pg_net;

-- Store the shared secret once. It must equal POLL_SECRET in Vercel.
select vault.create_secret('<paste POLL_SECRET here>', 'poller_secret');

select cron.schedule(
  'social-poll',
  '*/5 * * * *',
  $$
  select net.http_post(
    url := 'https://<deployment-host>/api/poll',
    headers := jsonb_build_object(
      'Authorization',
      'Bearer ' || (select decrypted_secret from vault.decrypted_secrets where name = 'poller_secret')
    ),
    body := '{}'::jsonb
  );
  $$
);
```

- Change the cadence by re-running `cron.schedule` with the same job name and a new expression, or `cron.unschedule('social-poll')` to stop it.
- `net.http_post` is fire-and-forget; responses are recorded in `net._http_response`, which is where to look if a run seems to fail. The function's own logs are in Vercel.
- Run the SQL only after the poller repo's deployment exists and its env vars are set.

## 4. Nothing else is needed in this repo

- No secrets from TikTok are needed in paper-doc-app. The TikTok client secret and database connection string live only in the poller's Vercel project.
- No OAuth callback route is needed. The poller repo has a local helper (`go run ./cmd/social-auth tiktok`) that handles the one-time authorization. The registered TikTok redirect URI can point at any https URL you control, since only the `code=` in the URL matters.

## 5. Order of operations

1. Run the migration above (this repo), in both preview and production databases.
2. In the poller repo: create the TikTok app, authorize each account with `social-auth` against each database, add them to `accounts.json` (see that repo's README).
3. Deploy the poller to Vercel with its env vars, and test it with the `curl` dry run in its README.
4. Create the `pg_cron` job (section 3) so there's data.
5. Build the dashboard against real rows.
