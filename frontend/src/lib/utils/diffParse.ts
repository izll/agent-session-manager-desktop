import type { session } from '../../../wailsjs/go/models';

/**
 * Turning diff text into lines the view can draw.
 *
 * Extracted from Diff.svelte, which is the largest component in the app and the
 * one a real bug hid in — the diff and its revert disagreeing about which
 * repository they meant. These two functions are pure: they read their
 * arguments and nothing else, which is precisely what makes them worth having
 * out here where a test can reach them without mounting a component.
 */

export type DiffLineType = 'add' | 'remove' | 'header' | 'context' | 'meta';

export interface ParsedDiffLine {
  text: string;
  type: DiffLineType;
}

/**
 * Classify each line of a diff body by what it says about the file.
 *
 * The `+++`/`---` exclusions matter: those are file headers, not an added and a
 * removed line, and colouring them as changes puts two bright bars at the top
 * of every file.
 */
export function parseDiff(content: string): ParsedDiffLine[] {
  return content.split('\n').map((line) => {
    let type: DiffLineType = 'context';
    if (line.startsWith('+') && !line.startsWith('+++')) {
      type = 'add';
    } else if (line.startsWith('-') && !line.startsWith('---')) {
      type = 'remove';
    } else if (line.startsWith('@@')) {
      type = 'header';
    } else if (line.startsWith('diff ') || line.startsWith('index ') ||
               line.startsWith('+++') || line.startsWith('---')) {
      type = 'meta';
    }
    return { text: line, type };
  });
}

export interface HunkView {
  hunk: session.DiffHunk;
  lines: ParsedDiffLine[];
  /** How many lines of this hunk were left undrawn. */
  hidden: number;
}

/**
 * Build the per-hunk line lists, with a budget shared across the whole file.
 *
 * The view renders one element per line with no virtualisation, so a very large
 * diff would insert tens of thousands of nodes synchronously and freeze the main
 * thread. The budget is spent in order, so the first hunks are drawn in full and
 * the later ones report what they left out rather than each hunk being truncated
 * to the same length.
 */
export function buildHunkViews(file: session.DiffFile, maxLines: number): HunkView[] {
  let budget = maxLines;
  return file.hunks.map((hunk) => {
    const all = parseDiff(hunk.body);
    const lines = budget > 0 ? all.slice(0, budget) : [];
    budget -= lines.length;
    return { hunk, lines, hidden: all.length - lines.length };
  });
}
