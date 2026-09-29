package whatsnew

import (
	"sort"

	"asmgr-desktop/updater"
)

// Launch says what the app should do about release notes as it starts.
type Launch struct {
	// Show asks for the dialog, opened on Versions.
	Show bool `json:"show"`
	// Since is the version whose notes the user saw last, the one updated
	// from. Empty when that is unknown: an install older than this feature.
	Since string `json:"since"`
	// Versions are the releases to show, newest first.
	Versions []string `json:"versions"`
	// Current is the running version.
	Current string `json:"current"`

	// Record is a version to store as seen straight away, with no dialog to
	// wait for: a fresh install, or an update whose releases have no notes.
	Record string `json:"-"`
}

// State is what is known at launch.
type State struct {
	Current string
	// LastSeen is the stored version, "" when there is none.
	LastSeen string
	// PriorInstall is true when the configuration existed before this run,
	// which tells an update from an install older than this feature apart
	// from a fresh install: neither has a LastSeen.
	PriorInstall bool
	// Dev is a development build. It carries the same version number as the
	// release it is heading for, so the number alone cannot tell.
	Dev bool
}

// Decide picks the releases to show at launch. The dialog lists every release
// newer than the last one seen, up to the running one — updating 1.1.14 to
// 1.1.18 shows 1.1.18, 1.1.17, 1.1.16 and 1.1.15.
//
//   - A development or pre-release build shows nothing and records nothing,
//     so the release installed beside it still shows its notes.
//   - A fresh install shows nothing: it records the running version.
//   - An install older than this feature (configuration present, nothing
//     recorded) shows the running version's notes, the one thing known to be
//     new; paging back reaches the rest.
//   - The same version, or a downgrade, shows nothing, and the record keeps
//     the newer version so going forward again does not repeat old notes.
func Decide(entries []Entry, s State) Launch {
	if s.Dev || !updater.IsRelease(s.Current) {
		return Launch{Current: s.Current}
	}
	out := Launch{Current: s.Current, Versions: []string{}}

	if !updater.IsRelease(s.LastSeen) {
		// Nothing usable recorded. A record that does not parse is treated
		// like a missing one: the configuration it lives in shows this is
		// not a fresh install.
		if !s.PriorInstall && s.LastSeen == "" {
			out.Record = s.Current
			return out
		}
		if hasVersion(entries, s.Current) {
			out.Show = true
			out.Versions = []string{s.Current}
		} else {
			out.Record = s.Current
		}
		return out
	}

	if cmp, _ := updater.CompareReleases(s.Current, s.LastSeen); cmp <= 0 {
		return out
	}
	out.Since = s.LastSeen
	out.Versions = Between(entries, s.LastSeen, s.Current)
	if len(out.Versions) == 0 {
		out.Record = s.Current
		return out
	}
	out.Show = true
	return out
}

// Between lists the releases after `after` up to and including `upTo`,
// newest first. Entries that are not release versions are left out.
func Between(entries []Entry, after, upTo string) []string {
	versions := []string{}
	for _, e := range entries {
		above, ok := updater.CompareReleases(e.Version, after)
		if !ok || above <= 0 {
			continue
		}
		if below, _ := updater.CompareReleases(e.Version, upTo); below > 0 {
			continue
		}
		versions = append(versions, e.Version)
	}
	sort.SliceStable(versions, func(i, j int) bool {
		cmp, _ := updater.CompareReleases(versions[i], versions[j])
		return cmp > 0
	})
	return versions
}

func hasVersion(entries []Entry, version string) bool {
	for _, e := range entries {
		if cmp, ok := updater.CompareReleases(e.Version, version); ok && cmp == 0 {
			return true
		}
	}
	return false
}
