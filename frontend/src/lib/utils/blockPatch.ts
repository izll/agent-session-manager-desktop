/**
 * A patch for one block of changes, cut out of a larger hunk.
 *
 * Reverting sends the hunk's patch back to `git apply --reverse` verbatim,
 * which is right when a hunk IS one change. In whole-file view it is not: git
 * is asked for the file as a single hunk (-U100000), so that patch covers every
 * change in the file, and reverting one block would throw away all of them.
 * That is why the arrows were disabled there — the button was inert rather than
 * wrong, but inert is still a button that does nothing.
 *
 * Cutting a block out means rebuilding the hunk around it: the block's own
 * added and removed lines, a few lines of untouched context either side, and a
 * header whose line numbers and counts describe exactly that. Anything else in
 * the hunk becomes context or is dropped, so git applies the one change and
 * leaves the rest of the file alone.
 */

/** A line as the diff view holds it. */
export interface PatchLine {
  /** 'add' | 'remove' | 'context' — anything else is a header and ignored. */
  type: string;
  /** The line's text, WITHOUT its leading +/-/space marker. */
  text: string;
}

/** How much untouched code to keep either side of the change. Three is what
 *  git itself uses by default, and it is enough for the patch to be located
 *  again if the file has shifted slightly. */
const CONTEXT = 3;

/**
 * Build a patch containing only the lines from `from` to `to`.
 *
 * `lines` is the whole hunk, `oldStart`/`newStart` the line numbers its header
 * declares. Returns null if the range holds no actual change — there would be
 * nothing to reverse, and an empty patch is an error rather than a no-op.
 */
export function buildBlockPatch(
  filePath: string,
  oldPath: string | null,
  lines: PatchLine[],
  oldStart: number,
  newStart: number,
  from: number,
  to: number,
): string | null {
  if (from < 0 || to < from || to >= lines.length) return null;

  /**
   * How far the real lines go.
   *
   * A hunk body ends with a newline, and the caller splits it on newlines, so
   * the last entry is always an empty string — sometimes two, since the diff
   * itself ends with a blank line. They parse as context, being neither + nor
   * -, and as context BELOW a block at the end of the file they were written
   * into the patch as empty lines the file does not have. git then refused the
   * whole thing: "patch does not apply".
   *
   * Trailing only: a genuinely empty line inside a file arrives here as a
   * single space (its context marker) and is not affected.
   */
  let end = lines.length;
  while (end > 0 && lines[end - 1].text === '' && lines[end - 1].type === 'context') end--;

  // Where the block sits in each file. Walking from the top of the hunk is the
  // only way to know: a line number advances on one side, the other, or both,
  // depending on what each line before it was.
  let oldAt = oldStart;
  let newAt = newStart;
  for (let at = 0; at < from; at++) {
    const type = lines[at].type;
    if (type === 'add') newAt++;
    else if (type === 'remove') oldAt++;
    else if (type === 'context') {
      oldAt++;
      newAt++;
    }
  }

  // The context above, taken from the lines immediately before the block. Only
  // unchanged lines qualify: a changed one belongs to a different block and
  // would drag that change in with this one.
  const before: PatchLine[] = [];
  for (let at = from - 1; at >= 0 && before.length < CONTEXT; at--) {
    if (lines[at].type !== 'context') break;
    before.unshift(lines[at]);
    oldAt--;
    newAt--;
  }

  // The block itself, and the context below it under the same rule.
  const body: PatchLine[] = [];
  let changes = 0;
  for (let at = from; at <= to; at++) {
    const type = lines[at].type;
    if (type !== 'add' && type !== 'remove' && type !== 'context') continue;
    if (type !== 'context') changes++;
    body.push(lines[at]);
  }
  if (changes === 0) return null;

  // Stopping at `end` rather than at lines.length: past it are only the empty
  // entries left by splitting the body, and they are not lines of the file.
  const after: PatchLine[] = [];
  for (let at = to + 1; at < end && after.length < CONTEXT; at++) {
    if (lines[at].type !== 'context') break;
    after.push(lines[at]);
  }

  const all = [...before, ...body, ...after];
  const oldCount = all.filter((line) => line.type !== 'add').length;
  const newCount = all.filter((line) => line.type !== 'remove').length;

  // A count of zero means the side has no lines at all, and git wants the start
  // line to be the one BEFORE the range in that case — a file created or
  // emptied by the change.
  const oldFrom = oldCount === 0 ? Math.max(0, oldAt - 1) : oldAt;
  const newFrom = newCount === 0 ? Math.max(0, newAt - 1) : newAt;

  const marker = (line: PatchLine) =>
    line.type === 'add' ? '+' : line.type === 'remove' ? '-' : ' ';

  // The a/ and b/ prefixes are what `git apply` expects, and a renamed file
  // needs its old name on the a/ side or the patch will not locate it.
  const a = oldPath || filePath;
  return [
    `diff --git a/${a} b/${filePath}`,
    `--- a/${a}`,
    `+++ b/${filePath}`,
    `@@ -${oldFrom},${oldCount} +${newFrom},${newCount} @@`,
    ...all.map((line) => `${marker(line)}${line.text}`),
    '',
  ].join('\n');
}
