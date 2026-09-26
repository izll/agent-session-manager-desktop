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

/**
 * A character reference starting exactly here — the same forms htmlToText
 * decodes. Anything else beginning with & is ordinary text.
 */
const ENTITY_AT = /&(#x[0-9a-f]+|#\d+|[a-z]+);/iy;

/** One visible character of a highlighted line, and the markup it came from. */
interface TextUnit {
  /** The markup: one character, or one whole entity — never part of one. */
  source: string;
  /** What htmlToText makes of it, lower-cased for matching. */
  lower: string;
  /** Its offset in htmlToText's output. */
  at: number;
}

/**
 * The line split into tags (kept verbatim) and text units (one character or
 * one entity each), in order. A "<" with no ">" after it is text, as it is to
 * htmlToText.
 */
function splitMarkup(html: string): Array<TextUnit | string> {
  const parts: Array<TextUnit | string> = [];
  let at = 0;
  let i = 0;
  while (i < html.length) {
    if (html[i] === '<') {
      const end = html.indexOf('>', i);
      if (end >= 0) {
        parts.push(html.slice(i, end + 1));
        i = end + 1;
        continue;
      }
    }
    let source: string;
    let text: string;
    if (html[i] === '&') {
      ENTITY_AT.lastIndex = i;
      const entity = ENTITY_AT.exec(html);
      source = entity ? entity[0] : '&';
      text = entity ? htmlToText(source) : source;
    } else {
      // A whole code point: a surrogate pair is one character to the reader.
      source = String.fromCodePoint(html.codePointAt(i) ?? 0);
      text = source;
    }
    parts.push({ source, lower: text.toLowerCase(), at });
    at += text.length;
    i += source.length;
  }
  return parts;
}

/**
 * The line's markup with every occurrence of the query wrapped in
 * `<mark class="…">`, so the match itself stands out and not only its row —
 * on an added or removed line the row tint alone leaves the eye to hunt.
 *
 * Matching follows findMatches: the text as the reader sees it (tags dropped,
 * entities decoded), case ignored. Characters before `from` in that text are
 * not searched — the +/- column, which the find does not match either.
 *
 * The markup is never broken: tags pass through untouched and what is inside
 * them is never matched; an entity is marked whole or not at all; and a match
 * that runs across a tag — two differently coloured tokens — is closed before
 * the tag and reopened after it, so the marks sit inside the spans rather than
 * straddling them. Nothing is decoded into the output: it is the input plus
 * the mark tags, so it is exactly as safe for {@html} as the input was.
 *
 * Overlapping occurrences ("aa" in "aaa") are merged into one mark; adjacent
 * ones stay separate. A blank query, or one that does not occur, returns the
 * input string itself.
 */
export function markMatchesInHtml(html: string, query: string, className: string, from = 0): string {
  if (!html || !query.trim()) return html;
  const needle = query.toLowerCase();
  const parts = splitMarkup(html);
  const units = parts.filter((part): part is TextUnit => typeof part !== 'string');

  // The lower-cased text, and for each of its characters the unit it came from.
  // Built per unit rather than by lower-casing the whole: that can change a
  // length, and the positions would drift.
  let lower = '';
  const owner: number[] = [];
  units.forEach((unit, index) => {
    lower += unit.lower;
    for (let k = 0; k < unit.lower.length; k++) owner.push(index);
  });

  // Each unit's match number, or -1. An overlap extends the previous match.
  const matchOf = new Array<number>(units.length).fill(-1);
  let matches = 0;
  let lastEnd = -1;
  for (let start = lower.indexOf(needle); start >= 0; start = lower.indexOf(needle, start + 1)) {
    if (units[owner[start]].at < from) continue;
    const end = start + needle.length;
    const id = start < lastEnd ? matches - 1 : matches++;
    for (let k = start; k < end; k++) matchOf[owner[k]] = id;
    lastEnd = end;
  }
  if (!matches) return html;

  const open = `<mark class="${escapeAttribute(className)}">`;
  let out = '';
  let unitIndex = 0;
  let inside = -1;
  for (const part of parts) {
    if (typeof part === 'string') {
      if (inside >= 0) out += '</mark>';
      inside = -1;
      out += part;
      continue;
    }
    const id = matchOf[unitIndex++];
    if (id !== inside) {
      if (inside >= 0) out += '</mark>';
      if (id >= 0) out += open;
      inside = id;
    }
    out += part.source;
  }
  if (inside >= 0) out += '</mark>';
  return out;
}

/**
 * markMatchesInHtml for a diff line: whatever the highlighter drew in front of
 * the searched text — the +/- column — is left unmarked, so the find and the
 * marks agree about what a line contains.
 *
 * Worked out from the two texts rather than from the markup, because the
 * highlighter draws that column differently depending on the path it took.
 */
export function markDiffLine(html: string, text: string, query: string, className: string): string {
  if (!html || !query.trim()) return html;
  const skip = Math.max(0, htmlToText(html).length - diffLineText(text).length);
  return markMatchesInHtml(html, query, className, skip);
}

/** The classes for a matching line's marks; the current match's are stronger. */
export function findMarkClass(current: boolean): string {
  return current ? 'diff-find-mark current' : 'diff-find-mark';
}

function escapeAttribute(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
}
