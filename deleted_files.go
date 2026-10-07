package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"asmgr-desktop/session"
)

// Files deleted from the file browser, held while the deletion can be undone.
//
// The interface offers the undo for ten seconds and then says so
// (DiscardDeletedSessionFile). The hold here is longer, and only a safety net:
// for an interface that went away without saying anything. A file still held
// when the app quits is removed then; one left over by a crash, at the next
// start.

// deletedFileHold is how long a deleted file is kept when nobody discards it.
const deletedFileHold = 2 * time.Minute

// staleDeletedFileAge is when a held file found at startup is a leftover. Not
// zero: another instance of the app, on another project, may be holding one
// right now.
const staleDeletedFileAge = time.Hour

type heldFile struct {
	sessionID string
	file      *session.DeletedFile
	timer     *time.Timer
}

type deletedFileHolds struct {
	mu   sync.Mutex
	held map[string]*heldFile
}

// DeleteSessionFile deletes one file of the browsed tree and returns the
// token that undoes it. Guarded like a save: it changes the project's files.
func (a *App) DeleteSessionFile(id, path string, windowIdx int, expectedRoot, expectedProjectID string) (string, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return "", err
	}
	defer done()
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
	if err != nil {
		return "", err
	}
	inst.BrowseRoot = resolvedRoot
	deleted, err := inst.DeleteFileForBrowse(path, a.storage.DeletedFilesDir())
	if err != nil {
		return "", err
	}
	a.InvalidateSessionFileIndex(id)
	return a.deletedFiles.hold(id, deleted), nil
}

// RestoreDeletedSessionFile undoes a DeleteSessionFile. A failed restore keeps
// the file held, so the undo can be tried again.
func (a *App) RestoreDeletedSessionFile(token string) error {
	held := a.deletedFiles.take(token)
	if held == nil {
		return fmt.Errorf("the deleted file is no longer kept")
	}
	if err := session.RestoreDeletedFile(held.file); err != nil {
		a.deletedFiles.put(token, held)
		if errors.Is(err, session.ErrRestoreOccupied) {
			return fmt.Errorf("%s exists again; it was not overwritten", filepath.Base(held.file.Original))
		}
		return err
	}
	a.InvalidateSessionFileIndex(held.sessionID)
	return nil
}

// DiscardDeletedSessionFile ends the undo offer: the file is removed for good.
func (a *App) DiscardDeletedSessionFile(token string) {
	if held := a.deletedFiles.take(token); held != nil {
		discardHeldFile(held)
	}
}

func discardHeldFile(held *heldFile) {
	if err := session.DiscardDeletedFile(held.file); err != nil {
		log.Printf("[files] could not remove the deleted %s: %v", held.file.Original, err)
	}
}

func (h *deletedFileHolds) hold(sessionID string, file *session.DeletedFile) string {
	token := newDeletedFileToken()
	h.put(token, &heldFile{sessionID: sessionID, file: file})
	return token
}

// put (re)arms the safety-net timer, so a restore that failed and was put
// back gets the full hold again.
func (h *deletedFileHolds) put(token string, held *heldFile) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held == nil {
		h.held = map[string]*heldFile{}
	}
	held.timer = time.AfterFunc(deletedFileHold, func() {
		if expired := h.takeIf(token, held); expired != nil {
			discardHeldFile(expired)
		}
	})
	h.held[token] = held
}

func (h *deletedFileHolds) take(token string) *heldFile {
	h.mu.Lock()
	defer h.mu.Unlock()
	held := h.held[token]
	if held == nil {
		return nil
	}
	held.timer.Stop()
	delete(h.held, token)
	return held
}

// takeIf takes the entry only if it is still the one the timer was armed for.
func (h *deletedFileHolds) takeIf(token string, want *heldFile) *heldFile {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held[token] != want {
		return nil
	}
	delete(h.held, token)
	return want
}

// discardAll removes every held file, at shutdown.
func (h *deletedFileHolds) discardAll() {
	h.mu.Lock()
	all := h.held
	h.held = nil
	h.mu.Unlock()
	for _, held := range all {
		held.timer.Stop()
		discardHeldFile(held)
	}
}

// purgeStaleDeletedFiles removes what a crashed run left in the holding
// directory.
func purgeStaleDeletedFiles(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < staleDeletedFileAge {
			continue
		}
		os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}

func newDeletedFileToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
