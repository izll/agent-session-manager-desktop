import { writable, get } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import { loadSessions } from './sessions';
import { activeProjectId } from './projects';

// The folder a session's diff shows, when it is not the session's own.
//
// Chosen from two places — the diff's header and the session's menu, which is
// the only way in while the session's own folder is not a repository and the
// diff cannot be opened — and both the diff and the Diff button have to notice.

/** Bumped whenever a session's diff folder changes. */
export const diffFolderVersion = writable(0);

/** Makes `dir` the session's diff folder; empty goes back to the session's. */
export async function setDiffFolder(sessionId: string, dir: string): Promise<void> {
  await App.SetSessionDiffDir(sessionId, dir, get(activeProjectId));
  diffFolderVersion.update((n) => n + 1);
  await loadSessions();
}

/**
 * Asks for a folder, starting in `startIn`, and makes it the session's diff
 * folder. False when the picker was cancelled.
 */
export async function chooseDiffFolder(sessionId: string, startIn: string): Promise<boolean> {
  const picked = await App.BrowseDirectory(startIn);
  if (!picked) return false;
  await setDiffFolder(sessionId, picked);
  return true;
}
