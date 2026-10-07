// Package schedule decides how often to snapshot, so a new post is watched
// closely and an old one only occasionally. The poller is triggered every few
// minutes; these rules decide which of those runs actually store a snapshot.
package schedule

import "time"

// Slack lets a snapshot count as due slightly early. Runs never land on exact
// intervals, and without slack an hourly snapshot could slip a whole run.
const Slack = time.Minute

const (
	hotUntil  = time.Hour      // posts younger than this are "hot"
	warmUntil = 48 * time.Hour // posts younger than this are "warm"

	hotEvery  = 5 * time.Minute
	warmEvery = time.Hour
	coldEvery = 24 * time.Hour

	// AccountEvery is how often account-level stats (followers) are stored.
	AccountEvery = time.Hour
)

// PostEvery is the minimum gap between snapshots of a post of the given age.
func PostEvery(age time.Duration) time.Duration {
	switch {
	case age < hotUntil:
		return hotEvery
	case age < warmUntil:
		return warmEvery
	default:
		return coldEvery
	}
}

// Due reports whether enough time has passed since the last snapshot.
func Due(sinceLast, every time.Duration) bool {
	return sinceLast >= every-Slack
}
