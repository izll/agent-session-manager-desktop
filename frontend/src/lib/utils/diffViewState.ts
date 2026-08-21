/**
 * Where the diff view was left, so switching tabs and back resumes it.
 *
 * The Diff component is destroyed on a tab switch — the mount points are in
 * exclusive branches — so its scroll position and the change it was stepping
 * through die with it. Coming back reset the file to the top, which in a long
 * review means finding your place again on every glance at the terminal.
 *
 * Module state rather than a store or a setting: nothing renders from it, so a
 * store would only add subscriptions, and it is worth exactly as long as the
 * window is open. The file that was open IS persisted (settings.diffLastFile),
 * because that survives a restart usefully; a scroll offset into a diff that
 * may have changed underneath does not.
 *
 * Keyed by session and file, so two sessions reviewed in turn keep their own
 * places.
 */

/** What a single file's review was in the middle of. */
export interface FileState {
  /** The scroller's offset in px. */
  scrollTop: number;
  /** Which change the stepping was on, and which one the marker named. -1 for
   *  none, as in the component. */
  currentHunk: number;
  markedHunk: number;
}

const places = new Map<string, FileState>();

/**
 * The field separator: a unit separator, which cannot occur in a session id or
 * a path, so no two different target/view/path tuples can spell the same key. Written as an
 * escape because the character itself is invisible in an editor.
 */
const SEP = '\x1f';

function key(projectId: string, sessionId: string, windowIdx: number, path: string, mode: string, root: string): string {
  // The mode is part of the key: the whole-file and hunks-only views lay the
  // same file out differently, so an offset from one means nothing in the other.
  return `${projectId}${SEP}${sessionId}${SEP}${windowIdx}${SEP}${mode}${SEP}${root}${SEP}${path}`;
}

/** Remember where a file was left. */
export function rememberPlace(
  sessionId: string,
  path: string,
  mode: string,
  state: FileState,
  windowIdx = 0,
  root = '',
  projectId = '',
): void {
  if (!sessionId || !path) return;
  places.set(key(projectId, sessionId, windowIdx, path, mode, root), state);
}

/** Where a file was left, or null if it has not been open. */
export function recallPlace(
  sessionId: string,
  path: string,
  mode: string,
  windowIdx = 0,
  root = '',
  projectId = '',
): FileState | null {
  if (!sessionId || !path) return null;
  return places.get(key(projectId, sessionId, windowIdx, path, mode, root)) ?? null;
}

/** Forget one tab's places without disturbing another repository in the session. */
export function forgetTarget(sessionId: string, windowIdx: number): void {
  if (!sessionId) return;
  for (const at of [...places.keys()]) {
    const parts = at.split(SEP);
    if (parts[1] === sessionId && Number(parts[2]) === windowIdx) places.delete(at);
  }
}

/** Forget one repository root in a tab, retaining positions for another root
 * that the same terminal tab may have visited with `cd`. */
function forgetRootTarget(projectId: string, sessionId: string, windowIdx: number, root: string): void {
  if (!sessionId) return;
  const prefix = `${projectId}${SEP}${sessionId}${SEP}${windowIdx}${SEP}`;
  for (const at of [...places.keys()]) {
    if (!at.startsWith(prefix)) continue;
    const parts = at.split(SEP);
    if (parts[4] === root) places.delete(at);
  }
}

/**
 * Forget a session's places.
 *
 * Called when a diff is reloaded from disk: the file may have changed, and an
 * offset into the version just replaced would land somewhere arbitrary — the
 * point of remembering is to return to the same place, not the same number.
 */
export function forgetSession(sessionId: string): void {
  if (!sessionId) return;
  for (const at of [...places.keys()]) {
    if (at.split(SEP)[1] === sessionId) places.delete(at);
  }
}

/**
 * What the diff looked like when its places were recorded.
 *
 * Held here rather than in the component for the same reason the places are:
 * the component is destroyed on a tab switch, so a copy inside it comes back
 * empty. Empty, every list looks changed, and the first load after the switch
 * forgot the places it was meant to restore — the position never came back.
 *
 * Keyed by session, tab, and diff mode: tabs may point at different repositories
 * and session/full mode may have the same summary shape but different content.
 */
const listKeys = new Map<string, string>();

/**
 * Record the current shape of one tab's diff mode, and say whether it changed.
 *
 * The caller drops its caches on a change; the places go with them, since an
 * offset into a file that has been rewritten means nothing.
 */
export function noteListKey(sessionId: string, listKey: string, windowIdx = 0, mode = 'session', root = '', projectId = ''): boolean {
  if (!sessionId) return false;
  const target = `${projectId}${SEP}${sessionId}${SEP}${windowIdx}${SEP}${mode}${SEP}${root}`;
  const changed = listKeys.get(target) !== listKey;
  listKeys.set(target, listKey);
  if (changed) forgetRootTarget(projectId, sessionId, windowIdx, root);
  return changed;
}


/**
 * The last loaded diff, so returning to the view does not refetch it.
 *
 * Switching tabs unmounts the diff — its two mount points are in exclusive
 * branches — so every field inside it starts empty on the way back, including
 * the key that records what is already loaded. The list was cleared and
 * refetched on every return, which reads as the whole review restarting: the
 * files blink away and the pane lands back at the top before the new data
 * arrives.
 *
 * One entry, because only one diff is on screen at a time. Kept for half a
 * minute rather than indefinitely — a diff older than that may no longer
 * describe the working tree, and showing a stale one is worse than a brief
 * load.
 */
const DIFF_CACHE_TTL_MS = 30_000;

let diffCache: { key: string; at: number; payload: unknown } | null = null;

export function cacheDiff(key: string, payload: unknown, now = Date.now()): void {
  diffCache = { key, at: now, payload };
}

/** The cached diff for this key, or null when absent or too old. */
export function cachedDiff(key: string, now = Date.now()): unknown | null {
  if (!diffCache || diffCache.key !== key) return null;
  if (now - diffCache.at > DIFF_CACHE_TTL_MS) return null;
  return diffCache.payload;
}

/** Dropped after a revert or a mode change, where the old list is wrong. */
export function invalidateDiffCache(): void {
  diffCache = null;
}
