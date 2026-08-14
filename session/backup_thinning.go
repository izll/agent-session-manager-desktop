package session

import (
	"sort"
	"time"
)

// How far back each band reaches, and how much of it is kept.
//
// The old rule was "the newest 25, whatever they are". On an active day that
// was under a day of history — measured on a real config: 25 backups spanning
// nineteen hours, the last three seconds apart. Anything noticed the next
// morning was already gone.
//
// Thinning keeps the recent past dense and the distant past sparse, which is
// how people actually look for a lost change: exactly when, if it was minutes
// ago; roughly which day, if it was last week.
var backupBands = []struct {
	// Backups newer than this belong to the band.
	within time.Duration
	// One backup is kept per bucket of this size. Zero keeps all of them.
	bucket time.Duration
}{
	{within: time.Hour, bucket: 0},                 // the last hour: everything
	{within: 24 * time.Hour, bucket: time.Hour},    // today: one an hour
	{within: 14 * 24 * time.Hour, bucket: 24 * time.Hour}, // a fortnight: one a day
	{within: 90 * 24 * time.Hour, bucket: 7 * 24 * time.Hour}, // a quarter: one a week
}

// backupsToKeep decides which backups survive, given their timestamps.
//
// Returns the indices to keep, so the caller can delete the rest. Works on
// times rather than files to stay testable without a directory.
//
// The newest is always kept regardless of banding: it is the one a restore is
// most likely to want, and a rule that could delete it would be indefensible
// however tidy the arithmetic.
func backupsToKeep(times []time.Time, now time.Time) map[int]bool {
	keep := map[int]bool{}
	if len(times) == 0 {
		return keep
	}

	// Newest first, so the first backup seen in a bucket is the one kept —
	// the most recent state of that hour or day, rather than its first.
	order := make([]int, len(times))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return times[order[a]].After(times[order[b]])
	})

	keep[order[0]] = true

	seen := map[int64]bool{}
	for _, index := range order {
		age := now.Sub(times[index])
		if age < 0 {
			// A clock change can date a backup in the future; keeping it is
			// safer than deleting something whose age cannot be judged.
			keep[index] = true
			continue
		}

		band := -1
		for b, definition := range backupBands {
			if age <= definition.within {
				band = b
				break
			}
		}
		if band == -1 {
			// Older than every band: dropped. A backup from last year is not
			// what anyone reaches for, and it costs space indefinitely.
			continue
		}

		bucket := backupBands[band].bucket
		if bucket == 0 {
			keep[index] = true
			continue
		}

		// Bucketed on absolute time, not on age, so the boundaries do not
		// shift under the set every time this runs — which would make a
		// different backup survive on each pass and thin the history unevenly.
		slot := times[index].Truncate(bucket).UnixNano()
		// The band is part of the key: the same instant falls in different
		// buckets as it ages, and without this a backup kept as "one an hour"
		// would block the daily slot it later belongs to.
		key := slot*int64(len(backupBands)) + int64(band)
		if seen[key] {
			continue
		}
		seen[key] = true
		keep[index] = true
	}

	return keep
}
