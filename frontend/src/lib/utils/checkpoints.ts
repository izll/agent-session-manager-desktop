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
