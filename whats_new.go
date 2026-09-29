package main

import (
	_ "embed"
	"log"
	"sync"

	"asmgr-desktop/whatsnew"
)

// The release notes travel inside the binary, so the "what's new" dialog
// needs no network and always matches the version that is running.
//
//go:embed CHANGELOG.md
var changelogMarkdown []byte

var changelogEntries = sync.OnceValue(func() []whatsnew.Entry {
	return whatsnew.Parse(changelogMarkdown)
})

// whatsNewPriorInstall is sampled before main runs — before the log file, the
// session store or the update check can write anything — so it says whether
// an earlier run left its configuration behind.
var whatsNewPriorInstall bool

func init() {
	whatsNewPriorInstall = whatsnew.HadPriorInstall(whatsnew.ConfigDir())
}

// GetChangelog returns every release in the changelog, newest first.
func (a *App) GetChangelog() []whatsnew.Entry {
	return changelogEntries()
}

// WhatsNewOnLaunch says whether to show release notes as the app starts, and
// which. Nothing is recorded for a shown dialog until MarkWhatsNewSeen: a
// launch that ends before the dialog is read shows it again next time.
func (a *App) WhatsNewOnLaunch() whatsnew.Launch {
	return whatsNewOnLaunch(whatsnew.ConfigDir(), whatsnew.State{
		Current:      Version,
		PriorInstall: whatsNewPriorInstall,
		Dev:          isDevMode,
	})
}

func whatsNewOnLaunch(dir string, state whatsnew.State) whatsnew.Launch {
	state.LastSeen = whatsnew.LoadSeen(dir)
	launch := whatsnew.Decide(changelogEntries(), state)
	if launch.Record != "" {
		if err := whatsnew.MarkSeen(dir, launch.Record); err != nil {
			log.Printf("[whatsnew] could not record %s as seen: %v", launch.Record, err)
		}
	}
	return launch
}

// MarkWhatsNewSeen records the running version's notes as read. A development
// build records nothing, so the release installed beside it keeps its dialog.
func (a *App) MarkWhatsNewSeen() error {
	if isDevMode {
		return nil
	}
	return whatsnew.MarkSeen(whatsnew.ConfigDir(), Version)
}
