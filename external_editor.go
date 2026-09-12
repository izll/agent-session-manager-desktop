package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"asmgr-desktop/session"
)

// The editors looked for when the setting is empty, in the order they are
// tried. VS Code first because it is the one most people have; the forks take
// the same arguments, so nothing below depends on which was found.
var knownEditors = []string{"code", "cursor", "codium", "code-insiders", "windsurf"}

var (
	editorOnce sync.Once
	editorPath string
)

// detectEditor finds an editor on PATH, once per run.
//
// Cached because the result is asked for on every button press and PATH does
// not change under a running app. A user who installs an editor while the app
// is open restarts it — the alternative is a filesystem probe per click.
func detectEditor() string {
	editorOnce.Do(func() {
		for _, name := range knownEditors {
			if path, err := exec.LookPath(name); err == nil {
				editorPath = path
				return
			}
		}
	})
	return editorPath
}

// editorCommand resolves the editor to use: the configured one when set,
// otherwise whatever is on PATH.
//
// A configured value is taken as the user's decision and is not second-guessed
// — it may be a wrapper script or a path to a build that is not on PATH. It is
// still checked for existence, because the error otherwise arrives as a bare
// "file not found" from exec with no hint of which setting caused it.
func (a *App) editorCommand() (string, error) {
	_, _, settings, err := a.storage.LoadAllWithSettings()
	if err == nil && settings != nil && strings.TrimSpace(settings.ExternalEditor) != "" {
		configured := strings.TrimSpace(settings.ExternalEditor)
		path, lookErr := exec.LookPath(configured)
		if lookErr != nil {
			return "", fmt.Errorf("editor %q not found — check the setting in Settings › General", configured)
		}
		return path, nil
	}

	if path := detectEditor(); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("no editor found — install VS Code, or name one in Settings › General")
}

// DetectedEditor reports the editor that would be used, for the settings UI to
// show as the placeholder. Empty when nothing was found.
func (a *App) DetectedEditor() string {
	return filepath.Base(detectEditor())
}

// startEditor runs the editor without waiting for it.
//
// Release() rather than Wait(): an editor outlives the click that opened it,
// often by hours, and holding the process here would leak a goroutine per file
// opened. The cost is that the exit status is never seen, which is why the
// arguments are validated before we get here rather than after.
func startEditor(path string, args ...string) error {
	cmd := session.Command(path, args...)
	// Detach from the app's own streams: an editor that writes to stderr would
	// otherwise interleave with our log, and on Windows a pipe left open can
	// keep a console alive.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start %s: %w", filepath.Base(path), err)
	}
	return cmd.Process.Release()
}

// OpenFileInEditor opens one file from a session's tree in the external editor,
// at the given line when one is supplied.
//
// The path is resolved through the same containment check the file browser
// uses, so a relative path from the UI cannot reach outside the session tree.
func (a *App) OpenFileInEditor(id, path string, line int, windowIdx int, expectedRoot string) error {
	editor, err := a.editorCommand()
	if err != nil {
		return err
	}

	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return err
	}
	resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
	if err != nil {
		return err
	}
	inst.BrowseRoot = resolvedRoot

	abs, err := inst.ResolveBrowsePathForEditor(path)
	if err != nil {
		return err
	}

	// --goto takes "file:line", and ":0" would be read as a line number rather
	// than as "no preference", so the plain path is used when there is none.
	target := abs
	if line > 0 {
		target = fmt.Sprintf("%s:%d", abs, line)
	}
	// --reuse-window keeps the editor from opening a new window per file, which
	// is what makes this usable for repeated jumps out of the diff.
	return startEditor(editor, "--reuse-window", "--goto", target)
}

// OpenDiffInEditor opens one changed file in the editor's own diff view.
//
// The editor compares two files on disk, and the "before" side does not exist
// as one: it lives in git's object store. It is written to a temporary file
// first, named after the original so the editor's own tab title and syntax
// highlighting still make sense.
// mode is "session" (changes since the session started) or anything else for
// the uncommitted-changes view, matching the two tabs in the diff. The base
// commit is resolved here rather than passed in: it is the session's own
// recorded SHA, and the frontend has no business holding a git ref.
func (a *App) OpenDiffInEditor(id, path string, mode string, windowIdx int, expectedRoot string) error {
	editor, err := a.editorCommand()
	if err != nil {
		return err
	}

	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return err
	}
	resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
	if err != nil {
		return err
	}
	inst.BrowseRoot = resolvedRoot

	abs, err := inst.ResolveBrowsePathForEditor(path)
	if err != nil {
		return err
	}

	baseRef := ""
	if mode == "session" {
		if inst.BaseCommitSHA == "" {
			return fmt.Errorf("no base commit for this session — use the uncommitted-changes view")
		}
		baseRef = inst.BaseCommitSHA
	}

	original, err := inst.WriteDiffBaseToTemp(path, baseRef)
	if err != nil {
		return err
	}

	// The temporary file is deliberately NOT deleted here. The editor is still
	// starting when this returns, and removing the file from under it shows an
	// empty pane. They go to a directory of our own under the OS temp dir,
	// which is swept at startup — see sweepDiffTempDirs.
	return startEditor(editor, "--reuse-window", "--diff", original, abs)
}

// sweepDiffTempDirs removes the temporary "before" files left behind by
// OpenDiffInEditor.
//
// They cannot be deleted when the editor opens them — it is still starting —
// and there is no moment afterwards when we know the editor is done with one.
// Clearing them at startup instead means the most that is ever left on disk is
// one run's worth, and nothing is removed while an editor might still show it.
func sweepDiffTempDirs() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), session.DiffTempPrefix) {
			os.RemoveAll(filepath.Join(os.TempDir(), entry.Name()))
		}
	}
}

// OpenFolderInEditor opens a session's directory as a project in the external
// editor.
//
// The editor's own source-control view then shows every change at once, which
// is the thing the per-file buttons cannot do: they answer "this file", not
// "everything I have touched".
//
// The directory is the one the diff and the file browser already work against,
// so what opens is what the user was looking at — not the session's configured
// path, which a tab may have moved away from.
func (a *App) OpenFolderInEditor(id string, windowIdx int, expectedRoot string) error {
	editor, err := a.editorCommand()
	if err != nil {
		return err
	}

	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return err
	}
	resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
	if err != nil {
		return err
	}

	// A new window rather than --reuse-window: reusing it would replace
	// whatever project the user has open, and opening a project is the one
	// action here where that is a loss rather than a convenience.
	return startEditor(editor, resolvedRoot)
}
