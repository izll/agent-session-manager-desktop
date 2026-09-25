/**
 * The decisions behind the checkpoints dialog, kept free of imports so the
 * node tests can run them without Svelte, the stores or the Go bindings.
 */

/** The fields of a checkpoint this module reads. */
export interface CheckpointLike {
  label: string;
  kind: string;
  files: number;
  statsKnown: boolean;
}

/**
 * Why checkpoints cannot be offered for a tab, or '' when they can.
 *
 * Remote first: a tab on a server usually also looks like "not a repository"
 * from here, since the check runs against this computer's disk, and telling
 * the user to init a repository that already exists on the server would send
 * them the wrong way.
 */
export function checkpointsUnavailable(opts: { remote: boolean; isGitRepo: boolean }): '' | 'remote' | 'notRepository' {
  if (opts.remote) return 'remote';
  if (!opts.isGitRepo) return 'notRepository';
  return '';
}

/**
 * What a row is called. The label the user gave, else what made it — a
 * "before restore" checkpoint names itself, since nobody typed a label for it.
 */
export function checkpointName(
  checkpoint: CheckpointLike,
  texts: { untitled: string; beforeRestore: string },
): string {
  const label = (checkpoint.label ?? '').trim();
  if (label) return label;
  return checkpoint.kind === 'beforeRestore' ? texts.beforeRestore : texts.untitled;
}

/**
 * How the checkpoint compares with the files now. "same" is worth saying
 * outright: restoring such a checkpoint would do nothing, and knowing that
 * saves a confirm dialog.
 */
export function checkpointDifference(checkpoint: CheckpointLike): 'unknown' | 'same' | 'differs' {
  if (!checkpoint.statsKnown) return 'unknown';
  return checkpoint.files > 0 ? 'differs' : 'same';
}

/**
 * Whether an agent in the session is working right now — on any of its tabs,
 * since every tab of a session can write into the same tree.
 *
 * A restore while an agent writes can leave a mix of both states, so the
 * confirmation warns; it does not refuse, because "busy" is a guess read off
 * the screen, and a wrong refusal would make the undo unreachable.
 */
export function sessionAgentBusy(
  sessionId: string,
  activities: Record<string, string | undefined>,
  tabStatuses: Record<string, Array<{ activity?: string }> | undefined>,
): boolean {
  if (!sessionId) return false;
  if (activities[sessionId] === 'busy') return true;
  return (tabStatuses[sessionId] ?? []).some((tab) => tab?.activity === 'busy');
}

/**
 * What the dialog's "Clean up" can take: checkpoints older than a number of
 * days, or the "before restore" ones the restores left behind. A day count is
 * the choice's own value; the one non-numeric choice is the restores'.
 */
export const CHECKPOINT_CLEANUP_BEFORE_RESTORE = 'beforeRestore';
export const CHECKPOINT_CLEANUP_DAYS = [7, 30, 90] as const;
/** A month: old enough that nobody is still counting on it by accident. */
export const DEFAULT_CHECKPOINT_CLEANUP = '30';

export interface CheckpointCleanupRuleLike {
  olderThanDays: number;
  beforeRestoreOnly: boolean;
}

/**
 * The backend's rule for a cleanup choice. The "before restore" choice takes
 * them at any age — the backend still spares those from the last day, since
 * one of them may be the way back from a restore made a moment ago.
 * Anything unrecognised falls back to the default rather than to "everything".
 */
export function checkpointCleanupRule(choice: string): CheckpointCleanupRuleLike {
  if (choice === CHECKPOINT_CLEANUP_BEFORE_RESTORE) {
    return { olderThanDays: 0, beforeRestoreOnly: true };
  }
  const days = Number.parseInt(choice, 10);
  if ((CHECKPOINT_CLEANUP_DAYS as readonly number[]).includes(days)) {
    return { olderThanDays: days, beforeRestoreOnly: false };
  }
  return { olderThanDays: Number.parseInt(DEFAULT_CHECKPOINT_CLEANUP, 10), beforeRestoreOnly: false };
}

/**
 * The ages the automatic cleanup setting offers. Longer than the manual ones:
 * a cleanup that runs unattended should only take what is plainly stale.
 * 0 is off, the default.
 */
export const CHECKPOINT_AUTO_PRUNE_DAYS = [30, 90, 180] as const;

/**
 * The day counts to offer for the setting, 0 (off) first. A stored value that
 * is not one of the usual ones — set by hand in the config — is offered too,
 * so the control shows what is in effect instead of claiming "off".
 */
export function checkpointAutoPruneDayChoices(current: number | undefined): number[] {
  const days: number[] = [0, ...CHECKPOINT_AUTO_PRUNE_DAYS];
  if (current && current > 0 && !days.includes(current)) days.push(current);
  return days.sort((a, b) => a - b);
}
