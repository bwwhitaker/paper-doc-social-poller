package schedule

import (
	"testing"
	"time"
)

func TestPostEvery(t *testing.T) {
	tests := []struct {
		age  time.Duration
		want time.Duration
	}{
		{0, 5 * time.Minute},
		{59 * time.Minute, 5 * time.Minute},
		{time.Hour, time.Hour}, // boundary belongs to the next tier
		{47 * time.Hour, time.Hour},
		{48 * time.Hour, 24 * time.Hour},
		{30 * 24 * time.Hour, 24 * time.Hour},
	}
	for _, tt := range tests {
		if got := PostEvery(tt.age); got != tt.want {
			t.Errorf("PostEvery(%s) = %s, want %s", tt.age, got, tt.want)
		}
	}
}

func TestDue(t *testing.T) {
	tests := []struct {
		name      string
		sinceLast time.Duration
		every     time.Duration
		want      bool
	}{
		{"just snapshotted", 0, time.Hour, false},
		{"well before", 30 * time.Minute, time.Hour, false},
		{"just inside slack", time.Hour - Slack, time.Hour, true},
		{"just outside slack", time.Hour - Slack - time.Second, time.Hour, false},
		{"overdue", 3 * time.Hour, time.Hour, true},
		// Runs ~5 minutes apart land a few seconds short of 5m; slack covers it.
		{"5m tier with jitter", 5*time.Minute - 10*time.Second, 5 * time.Minute, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Due(tt.sinceLast, tt.every); got != tt.want {
				t.Errorf("Due(%s, %s) = %v, want %v", tt.sinceLast, tt.every, got, tt.want)
			}
		})
	}
}
