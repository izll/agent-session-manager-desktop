import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export interface GitBranchInfo {
  path: string;
  repository: boolean;
  branch: string;
  upstream: string;
  ahead: number;
  behind: number;
}

/** Branch of the currently selected tab's working directory, or null. */
export const gitBranch = writable<GitBranchInfo | null>(null);

export interface GitRepositoryTarget {
  projectId: string;
  sessionId: string;
  windowIdx: number;
  root: string;
}

/** Full browse identity the store currently describes. The same path can be
 * reused by another project/session, so path alone is not a safe reply key. */
let currentTarget: GitRepositoryTarget | null = null;
let inFlight = 0;

function targetKey(target: GitRepositoryTarget | null): string {
  return target
    ? `${target.projectId}\x1f${target.sessionId}\x1f${target.windowIdx}\x1f${target.root}`
    : '';
}

/**
 * Point the store at a path. Repeated calls for the same path are cheap on the
 * Go side (short-lived cache), so callers may fire this on every refresh
 * trigger without debouncing.
 */
export async function refreshGitBranch(projectId: string, sessionId: string, windowIdx: number, root: string) {
  if (!sessionId || !root) {
    currentTarget = null;
    inFlight++;
    gitBranch.set(null);
    return;
  }

  const target = { projectId, sessionId, windowIdx, root };
  const key = targetKey(target);

  // A path change invalidates whatever is on screen; a re-check of the same
  // path keeps the old value visible until the new one lands, so the branch
  // doesn't flicker away on every window focus.
  if (key !== targetKey(currentTarget)) {
    currentTarget = target;
    gitBranch.set(null);
  }

  const generation = ++inFlight;
  try {
    const info = await App.GetGitBranch(sessionId, windowIdx, root);
    if (generation !== inFlight || key !== targetKey(currentTarget)) return;
    gitBranch.set(info && info.repository ? (info as GitBranchInfo) : null);
  } catch (e) {
    console.error('Failed to read git branch:', e);
    if (generation === inFlight && key === targetKey(currentTarget)) gitBranch.set(null);
  }
}

/** Re-check the path already on screen, e.g. when the window regains focus. */
export async function revalidateGitBranch() {
  const target = currentTarget;
  if (target) await refreshGitBranch(target.projectId, target.sessionId, target.windowIdx, target.root);
}

export interface GitBranchEntry {
  name: string;
  hash: string;
  committed: string;
  current: boolean;
}

export interface GitBranchList {
  path: string;
  repository: boolean;
  branches: GitBranchEntry[];
  total: number;
  truncated: boolean;
}

/**
 * Read the local branches of a path. Deliberately not a store and not called on
 * render: the badge is mounted for every session, so listing must stay tied to
 * the user actually opening the dropdown.
 */
export async function listGitBranches(target: GitRepositoryTarget | null): Promise<GitBranchList> {
  const root = target?.root ?? '';
  const empty: GitBranchList = { path: root, repository: false, branches: [], total: 0, truncated: false };
  if (!target?.sessionId || !root) return empty;
  const list = await App.ListGitBranches(target.sessionId, target.windowIdx, root);
  return list ? (list as GitBranchList) : empty;
}

/** Snapshot the identity the badge currently describes. */
export function currentGitTarget(): GitRepositoryTarget | null {
  return currentTarget ? { ...currentTarget } : null;
}

/** Formats the ahead/behind suffix; empty when in sync or without an upstream. */
export function formatAheadBehind(info: GitBranchInfo | null): string {
  if (!info || !info.upstream) return '';
  const parts: string[] = [];
  if (info.ahead > 0) parts.push(`↑${info.ahead}`);
  if (info.behind > 0) parts.push(`↓${info.behind}`);
  return parts.join(' ');
}
