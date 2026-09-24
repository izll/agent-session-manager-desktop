// Decisions behind the branch badge's push and pull panels.
//
// Kept free of imports so the tests can load it directly: what is offered,
// and when the button may be pressed, is the part that must not be wrong for
// an action that publishes work or rewrites the working tree.

export type GitSyncDirection = 'push' | 'pull';

/** The part of the badge's branch snapshot these decisions read. */
export interface GitSyncBadgeState {
  upstream: string;
  behind: number;
  unpushed: number;
  unpushedKnown: boolean;
  onServer?: boolean;
}

export interface GitSyncPreviewState {
  direction: string;
  setUpstream: boolean;
  remotes: string[];
  remote: string;
  diverged: boolean;
  total: number;
}

/**
 * Whether "↑ N" opens the push panel. Only when the count is known — an
 * unknown count is no badge at all, and a push offered without one would be
 * a push of an unknown number of commits. Never for a tab on a server: the
 * badge reads that tab's directory on this computer, which is not the
 * checkout the tab works in.
 */
export function canOfferPush(info: GitSyncBadgeState | null | undefined): boolean {
  return !!info && !info.onServer && info.unpushedKnown && info.unpushed > 0;
}

/** Whether "↓ N" opens the pull panel. Behind needs an upstream to exist. */
export function canOfferPull(info: GitSyncBadgeState | null | undefined): boolean {
  return !!info && !info.onServer && !!info.upstream && info.behind > 0;
}

/**
 * The remote a set-upstream push goes to: the user's pick when they made one,
 * else the backend's suggestion. '' until there is one — several remotes and
 * no obvious choice is the user's to make, never a guess.
 */
export function chosenRemote(preview: GitSyncPreviewState | null, picked: string): string {
  if (!preview || !preview.setUpstream) return '';
  if (picked && preview.remotes.includes(picked)) return picked;
  return preview.remotes.includes(preview.remote) ? preview.remote : '';
}

/**
 * Whether the panel's button may be pressed. Nothing to move, a pull that
 * cannot fast-forward, and a set-upstream push with no remote chosen are all
 * refused here rather than left to fail.
 */
export function canRunSync(preview: GitSyncPreviewState | null, picked: string, busy: boolean): boolean {
  if (!preview || busy || preview.total <= 0) return false;
  if (preview.direction === 'pull') return !preview.diverged;
  if (preview.setUpstream) return chosenRemote(preview, picked) !== '';
  return true;
}

const OUTCOMES = new Set([
  'pushed', 'pulled', 'upToDate', 'rejected', 'diverged', 'localChanges',
  'auth', 'timeout', 'cancelled', 'changed', 'failed',
]);

/** The translation key explaining a push or pull outcome. */
export function outcomeKey(outcome: string): string {
  return `gitSync.outcome.${OUTCOMES.has(outcome) ? outcome : 'failed'}`;
}
