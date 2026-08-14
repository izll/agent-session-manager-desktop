package session

import (
	"testing"
	"time"
)

// The newest backup survives whatever else the rules say.
//
// It is the one a restore is most likely to want, and a thinning rule that
// could delete it would be indefensible however tidy its arithmetic.
func TestTheNewestBackupIsAlwaysKept(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	// Everything far outside every band, so nothing else qualifies.
	times := []time.Time{
		now.Add(-400 * 24 * time.Hour),
		now.Add(-300 * 24 * time.Hour),
		now.Add(-200 * 24 * time.Hour),
	}

	keep := backupsToKeep(times, now)
	if !keep[2] {
		t.Error("the newest backup must be kept even when it is older than every band")
	}
}

// The last hour is kept in full.
//
// That is when a mistake is noticed and undone, and the difference between two
// saves a minute apart is exactly what someone is trying to get back to.
func TestRecentBackupsAreNotThinned(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	var times []time.Time
	for minutes := 0; minutes < 60; minutes += 5 {
		times = append(times, now.Add(-time.Duration(minutes)*time.Minute))
	}

	keep := backupsToKeep(times, now)
	if len(keep) != len(times) {
		t.Errorf("all %d backups from the last hour should be kept, got %d", len(times), len(keep))
	}
}

// Today's older backups collapse to one an hour.
//
// This is the case that broke the old rule: a busy day produced a backup every
// few seconds, and keeping the newest 25 meant the whole history spanned hours.
func TestTodayIsThinnedToOnePerHour(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	// Six hours of backups, one a minute — 360 of them, all older than an hour.
	var times []time.Time
	for minutes := 90; minutes < 90+360; minutes++ {
		times = append(times, now.Add(-time.Duration(minutes)*time.Minute))
	}

	keep := backupsToKeep(times, now)

	// Six or seven hourly slots, depending on where the boundaries fall.
	if len(keep) > 8 {
		t.Errorf("six hours of minute-by-minute backups should thin to about six, got %d", len(keep))
	}
	if len(keep) < 5 {
		t.Errorf("thinning took too much: %d left from six hours", len(keep))
	}
}

// A fortnight back, one a day is enough to find the day something went wrong.
func TestAFortnightKeepsOnePerDay(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	// Ten days, four backups a day.
	var times []time.Time
	for day := 2; day <= 11; day++ {
		for _, hour := range []int{2, 8, 14, 20} {
			times = append(times, now.Add(-time.Duration(day)*24*time.Hour).Add(time.Duration(hour)*time.Hour))
		}
	}

	keep := backupsToKeep(times, now)
	if len(keep) > 14 {
		t.Errorf("ten days at four a day should thin to about ten, got %d", len(keep))
	}
	if len(keep) < 8 {
		t.Errorf("thinning took too much: %d left from ten days", len(keep))
	}
}

// Thinning has to be stable: running it twice must not take a second bite.
//
// Buckets are computed on absolute time rather than on age for this reason. On
// age, the boundaries shift with every run, a different backup wins each time,
// and the history erodes unevenly on a schedule nobody chose.
func TestThinningIsStableAcrossRuns(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	var times []time.Time
	for hours := 2; hours < 20; hours++ {
		for _, offset := range []int{0, 20, 40} {
			times = append(times, now.Add(-time.Duration(hours)*time.Hour).Add(time.Duration(offset)*time.Minute))
		}
	}

	first := backupsToKeep(times, now)

	// The survivors, run through again a moment later.
	var survivors []time.Time
	for i, at := range times {
		if first[i] {
			survivors = append(survivors, at)
		}
	}
	second := backupsToKeep(survivors, now.Add(time.Minute))

	if len(second) != len(survivors) {
		t.Errorf("a second pass removed %d more backups; thinning must converge",
			len(survivors)-len(second))
	}
}

// A backup dated in the future is kept.
//
// A clock change can produce one, and its age cannot be judged — deleting on
// the strength of a nonsensical age is worse than keeping a spare file.
func TestBackupsFromTheFutureAreKept(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	times := []time.Time{
		now.Add(-2 * time.Hour),
		now.Add(48 * time.Hour), // clock was wrong when this was written
	}

	keep := backupsToKeep(times, now)
	if !keep[1] {
		t.Error("a backup dated in the future must be kept, not deleted on a negative age")
	}
}

// The timestamp is read from the filename, which is how the times above are
// obtained in the first place. A name that does not parse must not panic or
// claim a plausible date.
func TestBackupTimeReadsTheFilename(t *testing.T) {
	at := backupTime("20260814T100903.503416908Z-46093cb0.json")
	want := time.Date(2026, 8, 14, 10, 9, 3, 503416908, time.UTC)
	if !at.Equal(want) {
		t.Errorf("backupTime = %v, want %v", at, want)
	}

	if got := backupTime("not-a-backup.json"); !got.IsZero() {
		t.Errorf("an unparseable name should give the zero time, got %v", got)
	}
	if got := backupTime("short"); !got.IsZero() {
		t.Errorf("a name shorter than the layout should give the zero time, got %v", got)
	}
}
