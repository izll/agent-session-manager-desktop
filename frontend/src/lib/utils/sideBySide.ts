/**
 * Turning a unified diff into two aligned columns.
 *
 * A unified diff is one column with markers: a removed line, then the line
 * that replaced it, then the context they sit in. Read side by side the same
 * change is two columns with the pairs level with each other — which is what
 * makes "this became that" legible at a glance rather than reconstructed by
 * counting lines.
 *
 * The alignment is the whole job. Where one side has more lines than the
 * other, the shorter side needs blank rows to keep the pairs level; without
 * them the two columns drift apart and every comparison after the first change
 * is against the wrong line.
 */

/** A line as it appears in a unified diff. */
export interface UnifiedLine {
  type: string;
  html: string;
}

/** One row of the two-column view. Either side may be absent. */
export interface SideBySideRow {
  /** Line number in the original file, or null for an added line. */
  oldNumber: number | null;
  /** Line number in the new file, or null for a removed line. */
  newNumber: number | null;
  /** Rendered HTML for each side; null where that side has no line here. */
  oldHtml: string | null;
  newHtml: string | null;
  /** 'context' | 'change' | 'header' — what the row is, for styling. */
  kind: 'context' | 'change' | 'header';
}

/** The line numbers a hunk header declares. */
export function parseHunkHeader(header: string): { oldStart: number; newStart: number } {
  const match = header.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
  if (!match) return { oldStart: 1, newStart: 1 };
  return { oldStart: Number(match[1]), newStart: Number(match[2]) };
}

/**
 * Pair up a hunk's lines into rows.
 *
 * Removals and the additions that follow them are matched one for one, so a
 * line and its replacement share a row. Where the counts differ the surplus
 * lines get rows of their own with the other side blank — which is what makes
 * "three lines became one" visible as three rows against one.
 */
export function buildSideBySide(
  header: string,
  lines: UnifiedLine[],
): SideBySideRow[] {
  // The +/- column has no job here: which side a line is on says what happened
  // to it, and the marker would take a character off the width of both. Done
  // once, so every caller's lines arrive the same way.
  const strip = (html: string) => html.replace(/^<span style="opacity:0\.45">[+\- ]<\/span> /, '');
  const { oldStart, newStart } = parseHunkHeader(header);
  let oldNumber = oldStart;
  let newNumber = newStart;

  const rows: SideBySideRow[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];

    if (line.type === 'header' || line.type === 'meta') {
      rows.push({ oldNumber: null, newNumber: null, oldHtml: strip(line.html), newHtml: null, kind: 'header' });
      index++;
      continue;
    }

    if (line.type !== 'add' && line.type !== 'remove') {
      rows.push({
        oldNumber: oldNumber++,
        newNumber: newNumber++,
        oldHtml: strip(line.html),
        newHtml: strip(line.html),
        kind: 'context',
      });
      index++;
      continue;
    }

    // A run of changes: every removal, then every addition. git emits them in
    // that order within a hunk, and pairing across the whole run rather than
    // line by line is what lines a replacement up with what it replaced.
    const removed: UnifiedLine[] = [];
    const added: UnifiedLine[] = [];
    while (index < lines.length && lines[index].type === 'remove') removed.push(lines[index++]);
    while (index < lines.length && lines[index].type === 'add') added.push(lines[index++]);

    const pairs = Math.max(removed.length, added.length);
    for (let at = 0; at < pairs; at++) {
      rows.push({
        oldNumber: at < removed.length ? oldNumber++ : null,
        newNumber: at < added.length ? newNumber++ : null,
        oldHtml: at < removed.length ? strip(removed[at].html) : null,
        newHtml: at < added.length ? strip(added[at].html) : null,
        kind: 'change',
      });
    }
  }

  return rows;
}

/** One side's lines, with the row each belongs to. */
export interface SideLine {
  number: number | null;
  html: string | null;
  kind: 'context' | 'change' | 'header';
  /** Which paired row this line belongs to, for keeping the sides in step. */
  row: number;
}

/**
 * Split paired rows into two independent columns.
 *
 * The paired form fills the shorter side with blanks so both columns have the
 * same number of rows. That is what lets one grid hold them, and it is also
 * why a pure insertion shows a screenful of nothing on the left.
 *
 * Read as two separate columns instead, each holds only its own lines, and
 * they are kept in step by scrolling rather than by padding — so the side with
 * nothing to show simply waits while the other runs through the insertion,
 * which is what IntelliJ does and what makes a long insertion readable.
 */
export function splitSides(rows: SideBySideRow[]): { left: SideLine[]; right: SideLine[] } {
  const left: SideLine[] = [];
  const right: SideLine[] = [];

  rows.forEach((row, index) => {
    if (row.kind === 'header') {
      left.push({ number: null, html: row.oldHtml, kind: 'header', row: index });
      right.push({ number: null, html: row.oldHtml, kind: 'header', row: index });
      return;
    }
    if (row.oldHtml !== null) {
      left.push({ number: row.oldNumber, html: row.oldHtml, kind: row.kind, row: index });
    }
    if (row.newHtml !== null) {
      right.push({ number: row.newNumber, html: row.newHtml, kind: row.kind, row: index });
    }
  });

  return { left, right };
}

/**
 * Where the other side should sit when one side is scrolled to a given line.
 *
 * Both sides walk the same paired rows, so a line on one side names a row, and
 * that row names a position on the other. Where the other side has no line for
 * that row — an insertion, read from the side that lacks it — the answer is
 * the last line it does have: it stays put while the other runs on, and picks
 * up again when the run ends. That waiting is the whole point of two scrollers
 * rather than one grid.
 */
export function matchingLine(from: SideLine[], to: SideLine[], index: number): number {
  const row = from[Math.min(Math.max(index, 0), from.length - 1)]?.row;
  if (row === undefined) return 0;

  let best = 0;
  for (let at = 0; at < to.length; at++) {
    if (to[at].row > row) break;
    best = at;
  }
  return best;
}
