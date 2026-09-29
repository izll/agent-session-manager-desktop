import { get, writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

/**
 * Which files each repository keeps out of the diff view.
 *
 * Held here rather than in the diff component: two diffs can be on screen at
 * once — the full view and the one above a terminal — and hiding a file in
 * one has to take it out of the other at the same moment. Keyed by the
 * repository the backend resolved (the top of the working tree), so a tab
 * opened in a subdirectory shares the list with one at the root.
 *
 * The rules themselves are stored by the backend (package diffhidden), per
 * repository and not per session or project. Nothing is written to git.
 */
export const diffHiddenRules = writable<Record<string, string[]>>({});

/** Which repository a diff target belongs to, once asked. Resolving it costs
 *  a git call, and the diff reloads every few seconds. */
const repoForTarget = new Map<string, string>();

type Answer = { repo: string; rules: string[] | null };

function remember(target: string, answer: Answer): string {
  repoForTarget.set(target, answer.repo);
  diffHiddenRules.update((all) => ({ ...all, [answer.repo]: answer.rules ?? [] }));
  return answer.repo;
}

function targetKey(sessionId: string, windowIdx: number, root: string): string {
  return `${sessionId}\x1f${windowIdx}\x1f${root}`;
}

/** The repository a diff target belongs to, if already known — so a list
 *  shown from cache is shown with its hidden files already out of it. */
export function knownHiddenRepo(sessionId: string, windowIdx: number, root: string): string {
  const known = repoForTarget.get(targetKey(sessionId, windowIdx, root));
  return known !== undefined && known in get(diffHiddenRules) ? known : '';
}

/**
 * The repository a diff target belongs to, with its rules loaded.
 *
 * Answers from memory after the first time. '' when there is no repository
 * to key on — hiding is then simply not offered, and the diff is unaffected.
 */
export async function hiddenRepoFor(sessionId: string, windowIdx: number, root: string): Promise<string> {
  const key = targetKey(sessionId, windowIdx, root);
  const known = repoForTarget.get(key);
  if (known !== undefined && known in get(diffHiddenRules)) return known;
  try {
    return remember(key, await App.GetDiffHiddenRules(sessionId, windowIdx, root));
  } catch {
    return '';
  }
}

/** Hide a file, folder or pattern. Throws the backend's refusal. */
export async function hideInDiff(sessionId: string, windowIdx: number, root: string, rule: string): Promise<void> {
  remember(targetKey(sessionId, windowIdx, root), await App.AddDiffHiddenRule(sessionId, windowIdx, root, rule));
}

/** Show again what the given rules hid. Throws the backend's refusal. */
export async function showInDiff(sessionId: string, windowIdx: number, root: string, rules: string[]): Promise<void> {
  remember(targetKey(sessionId, windowIdx, root), await App.RemoveDiffHiddenRules(sessionId, windowIdx, root, rules));
}
