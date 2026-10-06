-- TikTok stats poller tables. Run once in the Supabase SQL editor.

create table tiktok_account_snapshots (
  id bigint generated always as identity primary key,
  captured_at timestamptz not null default now(),
  follower_count bigint not null,
  following_count bigint,
  likes_count bigint not null,
  video_count integer not null
);

create table tiktok_videos (
  video_id text primary key,            -- TikTok's video ID
  title text,
  video_description text,
  share_url text,
  duration_seconds integer,
  published_at timestamptz,
  first_seen_at timestamptz not null default now(),
  last_seen_at timestamptz not null default now()
);

create table tiktok_video_snapshots (
  id bigint generated always as identity primary key,
  video_id text not null references tiktok_videos (video_id),
  captured_at timestamptz not null default now(),
  view_count bigint not null,
  like_count bigint not null,
  comment_count bigint not null,
  share_count bigint not null
);

create index tiktok_video_snapshots_video_time
  on tiktok_video_snapshots (video_id, captured_at);

-- Holds the founder's OAuth tokens. RLS on, no policies,
-- so only the poller's direct connection can read it.
create table tiktok_oauth_tokens (
  open_id text primary key,             -- TikTok's ID for the authorized user
  access_token text not null,
  access_token_expires_at timestamptz not null,
  refresh_token text not null,
  refresh_token_expires_at timestamptz not null,
  updated_at timestamptz not null default now()
);

alter table tiktok_account_snapshots enable row level security;
alter table tiktok_videos enable row level security;
alter table tiktok_video_snapshots enable row level security;
alter table tiktok_oauth_tokens enable row level security;

-- TODO: add admin-only select policies on the first three tables,
-- matching how paper-doc-app's existing admin policies identify admins.
-- (Only needed if the dashboard reads these tables through Supabase's API
-- rather than through this repo's own server.)
