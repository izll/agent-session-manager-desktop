/**
 * Fuzzy ranking for the file browser's quick-open.
 *
 * The query is matched as a SUBSEQUENCE of the path — typing "flbrsv" finds
 * "components/MainPanel/FileBrowser.svelte" — because a user who half-remembers
 * a file remembers its initials and its extension, not a contiguous slice of
 * its name. A plain substring filter refuses all of those.
 *
 * The scoring is deliberately simple and greedy rather than an optimal
 * alignment: this runs over the whole index on every keystroke, and an
 * O(query x path) dynamic program over 20 000 paths is far too slow for the
 * webview. The greedy pass is O(path) per candidate and its ranking is
 * indistinguishable from optimal for real file paths, where the query's
 * characters appear in a handful of plausible places at most.
 */

/** One scored candidate, with the matched character positions for highlighting. */
export interface FileMatch {
  path: string;
  score: number;
  /** Indices into `path` that the query matched, ascending. */
  positions: number[];
}

/** The shape the quick-open needs from an index entry. */
export interface MatchCandidate {
  path: string;
  name: string;
}

// Scoring weights. Tuned against the ordering a developer expects rather than
// derived from anything: a hit in the basename beats a hit in the directory,
// consecutive characters beat scattered ones, and a shorter path wins a tie.
const SCORE_MATCH = 12;
// A run of adjacent matches is what makes "browser" beat "b...r...o...w"; the
// bonus grows with the run so a whole word matched intact dominates.
const SCORE_CONSECUTIVE = 20;
// Matching the character right after a separator, a dot, a dash or an
// underscore — or an interior capital — means the query hit a word boundary,
// which is almost always what the user was aiming at.
const SCORE_BOUNDARY = 16;
// Everything in the basename counts for more than anything in the directory:
// people search for a file, and remember its directory only vaguely.
const SCORE_BASENAME = 22;
// The very first character of the basename is the strongest signal of all.
const SCORE_BASENAME_START = 30;
// Per character of unmatched gap, so a query whose characters are spread across
// the whole path ranks below one that lands in a tight cluster. Capped so a
// long directory prefix cannot drive an otherwise perfect basename match
// negative.
const PENALTY_GAP = 3;
const MAX_GAP_PENALTY = 40;
// Per character of path length, breaking ties toward the shorter path.
const PENALTY_LENGTH = 0.4;

/**
 * Score one path against a lowercased query.
 *
 * Returns null when the query is not a subsequence of the path at all — the
 * caller drops those rather than showing a zero-scored row.
 */
export function scorePath(path: string, name: string, queryLower: string): FileMatch | null {
  if (queryLower === '') return { path, score: 0, positions: [] };

  const lower = path.toLowerCase();
  // Where the basename starts, so a match can be told apart from one in the
  // directory prefix without splitting the string.
  const baseStart = path.length - name.length;

  const positions: number[] = [];
  let score = 0;
  let pathIdx = 0;
  let lastMatch = -1;
  let run = 0;

  for (let q = 0; q < queryLower.length; q++) {
    const ch = queryLower[q];
    // A space in the query separates words rather than being matched: "file
    // browser" should find "FileBrowser.svelte", and the space itself appears
    // in essentially no source path.
    if (ch === ' ') continue;

    const found = lower.indexOf(ch, pathIdx);
    if (found < 0) return null;

    if (found === lastMatch + 1) {
      run++;
      score += SCORE_CONSECUTIVE * Math.min(run, 3);
    } else {
      run = 0;
      const gap = lastMatch < 0 ? 0 : found - lastMatch - 1;
      score -= Math.min(gap * PENALTY_GAP, MAX_GAP_PENALTY);
    }

    score += SCORE_MATCH;
    if (isBoundary(path, found)) score += SCORE_BOUNDARY;
    if (found >= baseStart) {
      score += SCORE_BASENAME;
      if (found === baseStart) score += SCORE_BASENAME_START;
    }

    positions.push(found);
    lastMatch = found;
    pathIdx = found + 1;
  }

  score -= path.length * PENALTY_LENGTH;
  return { path, score, positions };
}

/**
 * A word boundary is the start of the string, the character after a separator
 * or a common word break, or an interior capital in camelCase.
 */
function isBoundary(path: string, idx: number): boolean {
  if (idx === 0) return true;
  const prev = path[idx - 1];
  if (prev === '/' || prev === '.' || prev === '-' || prev === '_' || prev === ' ') return true;
  const ch = path[idx];
  // camelCase: an uppercase letter after a lowercase one starts a new word.
  return ch >= 'A' && ch <= 'Z' && prev >= 'a' && prev <= 'z';
}

/**
 * Rank the whole index against a query, best first.
 *
 * `limit` caps the returned rows — the picker shows a scrollable list, not the
 * whole repository, and sorting is the expensive part.
 */
export function rankFiles(
  candidates: readonly MatchCandidate[],
  query: string,
  limit = 100
): FileMatch[] {
  const q = query.trim().toLowerCase();
  if (q === '') {
    // No query: show the shortest paths, which are the top-level files a user
    // opening the picker most likely wants. Sorted by path within a length so
    // the list does not reshuffle as the index is rebuilt.
    return candidates
      .slice()
      .sort((a, b) => a.path.length - b.path.length || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
      .slice(0, limit)
      .map((c) => ({ path: c.path, score: 0, positions: [] }));
  }

  const out: FileMatch[] = [];
  for (const candidate of candidates) {
    const match = scorePath(candidate.path, candidate.name, q);
    if (match) out.push(match);
  }
  // Descending score, then shorter path, then alphabetical — the last two make
  // the order deterministic, so an unchanged query never reshuffles the list.
  out.sort(
    (a, b) =>
      b.score - a.score ||
      a.path.length - b.path.length ||
      (a.path < b.path ? -1 : a.path > b.path ? 1 : 0)
  );
  return out.slice(0, limit);
}

/** One run of text, flagged as matched or not, for rendering the highlight. */
export interface HighlightSegment {
  text: string;
  matched: boolean;
}

/**
 * Split a path into alternating matched/unmatched runs.
 *
 * Built here rather than in the markup: Svelte 3 cannot express this without a
 * helper anyway, and doing it once per visible row keeps it off the hot path
 * that ranks the whole index.
 */
export function highlightSegments(path: string, positions: readonly number[]): HighlightSegment[] {
  if (positions.length === 0) return [{ text: path, matched: false }];

  const segments: HighlightSegment[] = [];
  let cursor = 0;
  let i = 0;
  while (i < positions.length) {
    const start = positions[i];
    if (start > cursor) segments.push({ text: path.slice(cursor, start), matched: false });
    // Merge adjacent matched positions into one run so a matched word is a
    // single span rather than one per character.
    let end = start + 1;
    while (i + 1 < positions.length && positions[i + 1] === end) {
      end++;
      i++;
    }
    segments.push({ text: path.slice(start, end), matched: true });
    cursor = end;
    i++;
  }
  if (cursor < path.length) segments.push({ text: path.slice(cursor), matched: false });
  return segments;
}
