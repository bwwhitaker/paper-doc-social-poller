package poller

import (
	"testing"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
)

func TestBuildPlan(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	snap := platform.Snapshot{Posts: []platform.Post{
		{ID: "new-unseen", PublishedAt: ago(2 * time.Minute)},
		{ID: "hot-due", PublishedAt: ago(30 * time.Minute)},
		{ID: "hot-not-due", PublishedAt: ago(30 * time.Minute)},
		{ID: "warm-due", PublishedAt: ago(10 * time.Hour)},
		{ID: "warm-not-due", PublishedAt: ago(10 * time.Hour)},
		{ID: "cold-due", PublishedAt: ago(5 * 24 * time.Hour)},
		{ID: "cold-not-due", PublishedAt: ago(5 * 24 * time.Hour)},
		{ID: "no-publish-time-not-due"},
		{ID: "no-publish-time-due"},
	}}
	lastPosts := map[string]time.Time{
		"hot-due":                 ago(5 * time.Minute),
		"hot-not-due":             ago(2 * time.Minute),
		"warm-due":                ago(61 * time.Minute),
		"warm-not-due":            ago(20 * time.Minute),
		"cold-due":                ago(25 * time.Hour),
		"cold-not-due":            ago(3 * time.Hour),
		"no-publish-time-not-due": ago(3 * time.Hour),
		"no-publish-time-due":     ago(25 * time.Hour),
	}

	plan := buildPlan(now, snap, ago(2*time.Hour), lastPosts)

	want := map[string]bool{
		"new-unseen":              true,
		"hot-due":                 true,
		"hot-not-due":             false,
		"warm-due":                true,
		"warm-not-due":            false,
		"cold-due":                true,
		"cold-not-due":            false,
		"no-publish-time-not-due": false,
		"no-publish-time-due":     true,
	}
	for id, w := range want {
		if got := plan.Posts[id]; got != w {
			t.Errorf("post %s due = %v, want %v", id, got, w)
		}
	}
	if !plan.Account {
		t.Error("account snapshot should be due after 2h")
	}
}

func TestBuildPlanAccount(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last time.Time
		want bool
	}{
		{"never snapshotted", time.Time{}, true},
		{"10 minutes ago", now.Add(-10 * time.Minute), false},
		{"over an hour ago", now.Add(-61 * time.Minute), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := buildPlan(now, platform.Snapshot{}, tt.last, nil)
			if plan.Account != tt.want {
				t.Errorf("Account = %v, want %v", plan.Account, tt.want)
			}
		})
	}
}

func TestBuildPlanFuturePublishTime(t *testing.T) {
	// Clock skew can put a publish time slightly in the future. It must be
	// treated as age zero, not a negative age that picks a wrong tier.
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	snap := platform.Snapshot{Posts: []platform.Post{
		{ID: "p", PublishedAt: now.Add(time.Minute)},
	}}
	plan := buildPlan(now, snap, now, map[string]time.Time{"p": now.Add(-5 * time.Minute)})
	if !plan.Posts["p"] {
		t.Error("age 0 should use the 5-minute tier, so a 5-minute-old snapshot is due")
	}
}
