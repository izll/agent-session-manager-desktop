/**
 * Find in a diff, whichever way it is drawn.
 *
 * The diff has three renderers — two columns, the whole file, and the hunks
 * alone — and the find bar used to exist only in the first. The matching rules
 * live here so all three answer the same query the same way: on the text the
 * reader sees, ignoring case, without the markup the highlighter wraps it in
 * and without the +/- notation in front of it.
 *
 * Pure on purpose: no DOM. The whole-file view keeps off-screen rows out of the
 * document, so the search has to run against the data, not the markup.
 */

const ENTITIES: Record<string, string> = {
  amp: '&',
  lt: '<',
  gt: '>',
  quot: '"',
  apos: "'",
  nbsp: ' ',
};

/**
 * The text of a highlighted line, as the reader sees it.
 *
 * Tags are dropped — otherwise a search for "span" hits every coloured token —
 * and entities decoded, because the highlighter escapes < and &: a search for
 * "a < b" would never match the "a &lt; b" in the markup.
 */
export function htmlToText(html: string | null | undefined): string {
  if (!html) return '';
  return html
    .replace(/<[^>]*>/g, '')
    .replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (whole, name: string) => {
      if (name[0] === '#') {
        const code = name[1] === 'x' || name[1] === 'X'
          ? parseInt(name.slice(2), 16)
          : parseInt(name.slice(1), 10);
        return Number.isFinite(code) ? String.fromCodePoint(code) : whole;
      }
      return ENTITIES[name.toLowerCase()] ?? whole;
    });
}

/**
 * The searchable part of a raw diff line: the line without its +/-/space
 * marker.
 *
 * The marker is notation, not content. Left in, a search for "-" would hit
 * every removed line, and a search starting at column one would miss the lines
 * the marker sits in front of. Header lines ("@@ … @@") keep their text.
 */
export function diffLineText(text: string): string {
  if (!text) return '';
  if (text.startsWith('@@')) return text;
  if (text.startsWith('+++') || text.startsWith('---')) return text;
  const lead = text[0];
  return lead === '+' || lead === '-' || lead === ' ' ? text.slice(1) : text;
}

/**
 * Indices of the entries containing the query, ignoring case, in order.
 * A blank query matches nothing — an empty bar is not a search.
 */
export function findMatches(texts: readonly string[], query: string): number[] {
  if (!query.trim()) return [];
  const needle = query.toLowerCase();
  const hits: number[] = [];
  texts.forEach((text, index) => {
    if (text.toLowerCase().includes(needle)) hits.push(index);
  });
  return hits;
}

/**
 * The next position in a list of `count` matches, wrapping at both ends.
 *
 * -1 is "not on any match yet" (the list was rebuilt under the reader and the
 * view was left where it was): forward then lands on the first, back on the
 * last.
 */
export function stepMatch(at: number, direction: 1 | -1, count: number): number {
  if (count <= 0) return -1;
  if (at < 0 || at >= count) return direction === 1 ? 0 : count - 1;
  return (at + direction + count) % count;
}

/**
 * Where the cursor goes when the matches are recomputed under it — the file
 * refreshed, or the view switched renderer.
 *
 * It stays on the same row if that row still matches, and otherwise is on no
 * match at all: jumping to the first one would scroll away from what the
 * reader was looking at, for a change they did not make.
 */
export function keepMatch(hits: readonly number[], current: number): number {
  return current < 0 ? -1 : hits.indexOf(current);
}

/** The "n/m" text for the bar. */
export function matchCounter(at: number, count: number, query: string, noMatches: string): string {
  if (count) return `${at >= 0 ? at + 1 : '–'}/${count}`;
  return query ? noMatches : '';
}

/**
 * What a key does in the find field: close the bar, step forward (1) or back
 * (-1), or nothing (null — let the field have it).
 *
 * Up and down step too: the field is one line, so they do nothing in it, and
 * reaching for them is the natural move once there is a list to walk.
 */
export function findKeyAction(event: {
  key: string;
  shiftKey?: boolean;
  ctrlKey?: boolean;
  metaKey?: boolean;
}): 'close' | 1 | -1 | null {
  if (event.key === 'Escape') return 'close';
  if (event.key === 'Enter' || event.key === 'F3' ||
      ((event.ctrlKey || event.metaKey) && event.key === 'g')) {
    return event.shiftKey ? -1 : 1;
  }
  if (event.key === 'ArrowDown') return 1;
  if (event.key === 'ArrowUp') return -1;
  return null;
}
