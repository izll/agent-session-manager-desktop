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

/** The path the store currently describes; also the key for stale-reply drops. */
let currentPath = '';
let inFlight = 0;

/**
 * Point the store at a path. Repeated calls for the same path are cheap on the
 * Go side (short-lived cache), so callers may fire this on every refresh
 * trigger without debouncing.
 */
export async function refreshGitBranch(path: string) {
  if (!path) {
    currentPath = '';
    gitBranch.set(null);
    return;
  }

  // A path change invalidates whatever is on screen; a re-check of the same
  // path keeps the old value visible until the new one lands, so the branch
  // doesn't flicker away on every window focus.
  if (path !== currentPath) {
    currentPath = path;
    gitBranch.set(null);
  }

  const generation = ++inFlight;
  try {
    const info = await App.GetGitBranch(path);
    if (generation !== inFlight || path !== currentPath) return;
    gitBranch.set(info && info.repository ? (info as GitBranchInfo) : null);
  } catch (e) {
    console.error('Failed to read git branch:', e);
    if (generation === inFlight && path === currentPath) gitBranch.set(null);
  }
}

/** Re-check the path already on screen, e.g. when the window regains focus. */
export async function revalidateGitBranch() {
  const path = currentPath;
  if (path) await refreshGitBranch(path);
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
export async function listGitBranches(path: string): Promise<GitBranchList> {
  const empty: GitBranchList = { path, repository: false, branches: [], total: 0, truncated: false };
  if (!path) return empty;
  const list = await App.ListGitBranches(path);
  return list ? (list as GitBranchList) : empty;
}

/** The path the badge currently describes, for the dropdown to list against. */
export function currentGitPath(): string {
  return currentPath;
}

/** Formats the ahead/behind suffix; empty when in sync or without an upstream. */
export function formatAheadBehind(info: GitBranchInfo | null): string {
  if (!info || !info.upstream) return '';
  const parts: string[] = [];
  if (info.ahead > 0) parts.push(`↑${info.ahead}`);
  if (info.behind > 0) parts.push(`↓${info.behind}`);
  return parts.join(' ');
}
