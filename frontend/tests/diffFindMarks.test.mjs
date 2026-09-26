import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * The find marks the matched text, not only its row.
 *
 * "esetleg a szavakat kiemelhetnénk, mert pl. ha zöld a háttér (pluszos sor)
 * ott keresni kell szemmel": on an added or removed row the row tint says the
 * line matches but not where, and over green or red the eye has to hunt.
 *
 * The lines are syntax-highlighted, already-escaped HTML, so the marking walks
 * tags and text separately (utils/diffFind.ts, markMatchesInHtml). These tests
 * pin that it never breaks the markup and never lets text become markup; the
 * browser suite (tests/browser/diff-find.spec.mjs) checks the renderers.
 */

const root = new URL('..', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8').replace(/\r\n/g, '\n');

async function compile(path, name) {
  const dir = mkdtempSync(join(tmpdir(), 'diff-find-marks-'));
  const js = join(dir, `${name}.mjs`);
  writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
    input: read(path),
    encoding: 'utf8',
    cwd: root.pathname,
  }));
  return import(js);
}

const { markMatchesInHtml: mark, markDiffLine, htmlToText, findMarkClass, findMatches, diffLineText } =
  await compile('src/lib/utils/diffFind.ts', 'diffFind');

const M = '<mark class="m">';
const E = '</mark>';

/** What the browser would make of it: the marks must not change the text. */
const sameText = (before, after) => assert.equal(htmlToText(after), htmlToText(before));

/** Tags balance: every <mark> closed, and never across another tag. */
function marksWellFormed(html) {
  const tags = html.match(/<\/?[a-z]+[^>]*>/gi) ?? [];
  let open = false;
  for (const tag of tags) {
    if (/^<mark\b/.test(tag)) {
      assert.equal(open, false, `nested mark in ${html}`);
      open = true;
    } else if (tag === '</mark>') {
      assert.equal(open, true, `stray </mark> in ${html}`);
      open = false;
    } else {
      assert.equal(open, false, `a mark straddles ${tag} in ${html}`);
    }
  }
  assert.equal(open, false, `unclosed mark in ${html}`);
}

test('the matched text is wrapped, the rest left alone', () => {
  assert.equal(mark('const needle = 5;', 'needle', 'm'), `const ${M}needle${E} = 5;`);
});

test('case is ignored, as findMatches ignores it — and the original case is kept', () => {
  assert.equal(mark('return NEEDLE_250;', 'needle', 'm'), `return ${M}NEEDLE${E}_250;`);
  assert.equal(mark('Árvíztűrő tükörfúrógép', 'TÜKÖR', 'm'), `Árvíztűrő ${M}tükör${E}fúrógép`);
  assert.equal(mark('ÁRVÍZ', 'árvíz', 'm'), `${M}ÁRVÍZ${E}`);
});

test('a blank query, or one that does not occur, returns the very same string', () => {
  const html = '<span style="color:#c678dd">if</span> x';
  assert.equal(mark(html, '', 'm'), html);
  assert.equal(mark(html, '   ', 'm'), html);
  assert.equal(mark(html, 'zzz', 'm'), html);
  assert.equal(mark('', 'x', 'm'), '');
  assert.equal(markDiffLine(html, '+if x', '', 'm'), html);
});

test('text inside tags and attributes is never matched', () => {
  const html = '<span style="color:#c678dd">if</span> span';
  const out = mark(html, 'span', 'm');
  assert.equal(out, `<span style="color:#c678dd">if</span> ${M}span${E}`);
  assert.equal(mark(html, 'color', 'm'), html);
  assert.equal(mark(html, 'c678dd', 'm'), html);
});

test('a match across two coloured tokens is split at the tags, not across them', () => {
  const html = '<span style="color:#e06c75">foo</span><span style="color:#56b6c2">.</span><span style="color:#61afef">bar</span>()';
  const out = mark(html, 'oo.ba', 'm');
  assert.equal(out,
    `<span style="color:#e06c75">f${M}oo${E}</span>` +
    `<span style="color:#56b6c2">${M}.${E}</span>` +
    `<span style="color:#61afef">${M}ba${E}r</span>()`);
  marksWellFormed(out);
  sameText(html, out);
});

test('entities are matched as the characters they show and never split', () => {
  const html = 'if (a &lt; b &amp;&amp; c &gt; d) &quot;x&quot; &#39;y&#x27;';
  assert.equal(mark(html, 'a < b', 'm'), `if (${M}a &lt; b${E} &amp;&amp; c &gt; d) &quot;x&quot; &#39;y&#x27;`);
  assert.equal(mark(html, '&&', 'm'), `if (a &lt; b ${M}&amp;&amp;${E} c &gt; d) &quot;x&quot; &#39;y&#x27;`);
  assert.equal(mark(html, '"x"', 'm'), `if (a &lt; b &amp;&amp; c &gt; d) ${M}&quot;x&quot;${E} &#39;y&#x27;`);
  assert.equal(mark(html, "'y'", 'm'), `if (a &lt; b &amp;&amp; c &gt; d) &quot;x&quot; ${M}&#39;y&#x27;${E}`);
  // The entity's own letters are not the text: "amp" and "lt" are not in this line.
  assert.equal(mark(html, 'amp', 'm'), html);
  assert.equal(mark(html, 'lt', 'm'), html);
  assert.equal(mark(html, 'quot', 'm'), html);
  for (const q of ['a < b', '&&', '"x"', "'y'", '<', '>', '&']) {
    const out = mark(html, q, 'm');
    marksWellFormed(out);
    sameText(html, out);
  }
});

test('a query that looks like markup is text, and stays escaped', () => {
  const html = '&lt;script&gt;alert(1)&lt;/script&gt;';
  const out = mark(html, '<script>', 'm');
  assert.equal(out, `${M}&lt;script&gt;${E}alert(1)&lt;/script&gt;`);
  assert.doesNotMatch(out, /<script/i, 'text was unescaped into markup');
  assert.equal(mark(html, '</script>', 'm'), `&lt;script&gt;alert(1)${M}&lt;/script&gt;${E}`);
  // Nothing in the query reaches the output: only the input and the mark tags.
  assert.equal(mark('a "b" c', '"b"', 'm'), `a ${M}"b"${E} c`);
});

test('the class name cannot break out of its attribute', () => {
  const out = mark('abc', 'b', 'x" onmouseover="alert(1)');
  assert.equal(out, 'a<mark class="x&quot; onmouseover=&quot;alert(1)">b</mark>c');
});

test('every occurrence is marked; overlapping ones merge, adjacent ones stay apart', () => {
  assert.equal(mark('a needle and a Needle', 'needle', 'm'), `a ${M}needle${E} and a ${M}Needle${E}`);
  assert.equal(mark('aaa', 'aa', 'm'), `${M}aaa${E}`);
  assert.equal(mark('abab', 'ab', 'm'), `${M}ab${E}${M}ab${E}`);
  assert.equal(mark('xaaaax', 'aa', 'm'), `x${M}aaaa${E}x`);
});

test('astral characters and combining marks are kept whole', () => {
  const html = 'emoji 😀 here';
  const out = mark(html, '😀', 'm');
  assert.equal(out, `emoji ${M}😀${E} here`);
  assert.equal(mark('a&#x1F600;b', '😀', 'm'), `a${M}&#x1F600;${E}b`);
  // Lower-casing İ gives two characters; the positions after it must not drift.
  assert.equal(mark('İx needle', 'needle', 'm'), `İx ${M}needle${E}`);
});

test('a lone < or & in the text is text, as htmlToText reads it', () => {
  assert.equal(mark('a < b', '< b', 'm'), `a ${M}< b${E}`);
  assert.equal(mark('a & b', '& b', 'm'), `a ${M}& b${E}`);
});

// --- the diff marker ---------------------------------------------------------

/** What highlightLine emits for a diff line: the marker in a dimmed column. */
const markerColumn = (marker) => `<span style="opacity:0.45">${marker}</span> `;

test('the +/- column is not marked — it is not searched either', () => {
  const text = '+ - + x';
  const html = markerColumn('+') + ' - + x';
  // findMatches says the searched text is " - + x"; the marker is not in it.
  assert.equal(diffLineText(text), ' - + x');
  const out = markDiffLine(html, text, '+', 'm');
  assert.equal(out, `${markerColumn('+')} - ${M}+${E} x`);
  // Only what the find matched: a query found in the line is marked in it.
  assert.equal(markDiffLine(markerColumn('-') + 'return a;', '-return a;', 'RETURN', 'm'),
    `${markerColumn('-')}${M}return${E} a;`);
});

test('the marker is skipped on every path the highlighter takes', () => {
  // No grammar: the column, then the escaped text.
  assert.equal(markDiffLine(markerColumn('+') + 'x &lt; y', '+x < y', 'x', 'm'),
    `${markerColumn('+')}${M}x${E} &lt; y`);
  // The fallback that escapes the whole line, marker included.
  assert.equal(markDiffLine('+x + y', '+x + y', '+', 'm'), `+x ${M}+${E} y`);
  // A context line: its marker is a space.
  assert.equal(markDiffLine(markerColumn(' ') + ' a', '  a', ' a', 'm'), `${markerColumn(' ')}${M} a${E}`);
  // A header keeps its text; nothing is skipped.
  assert.equal(markDiffLine('@@ -1 +1 @@', '@@ -1 +1 @@', '@@', 'm'), `${M}@@${E} -1 +1 ${M}@@${E}`);
});

test('every line findMatches reports gets a mark', () => {
  const lines = ['+const needle = 5;', '-return NEEDLE_250;', ' a < b && "c"', '+x'];
  const escape = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const htmls = lines.map((l) => markerColumn(l[0]) + escape(l.slice(1)));
  for (const q of ['needle', 'a < b', '&& "', '5;', 'x']) {
    const hits = findMatches(lines.map(diffLineText), q);
    for (const i of hits) assert.match(markDiffLine(htmls[i], lines[i], q, 'm'), /<mark /, `${q} in ${lines[i]}`);
  }
});

test('the current match gets the stronger class', () => {
  assert.equal(findMarkClass(false), 'diff-find-mark');
  assert.equal(findMarkClass(true), 'diff-find-mark current');
});

// --- wiring -----------------------------------------------------------------

const diff = read('src/lib/components/MainPanel/Diff.svelte');
const sbs = read('src/lib/components/MainPanel/SideBySideDiff.svelte');
const virtual = read('src/lib/components/MainPanel/VirtualLines.svelte');
const history = read('src/lib/components/Dialogs/GitHistoryDialog.svelte');
const style = read('src/style.css');

test('the whole-file view marks the matching rows it renders, and only those', () => {
  assert.match(diff, /hitQuery=\{diffQuery\}/);
  assert.match(virtual, /export let hitQuery = '';/);
  assert.match(virtual,
    /\{@html hitLines\.has\(first \+ i\)\s*\? markDiffLine\(line\.html, line\.text, hitQuery, findMarkClass\(first \+ i === currentHit\)\)\s*: line\.html\}/);
});

test('the hunk list marks its matching rows', () => {
  assert.match(diff,
    /\{@html lineHitSet\.has\(hunkLineOffsets\[h\] \+ j\)\s*\? markDiffLine\(\s*memoHighlightLine\(line\.text, lineLanguage\),\s*line\.text,\s*diffQuery,\s*findMarkClass\(hunkLineOffsets\[h\] \+ j === currentLineHit\),?\s*\)\s*: memoHighlightLine\(line\.text, lineLanguage\)\}/);
});

test('both columns mark their side of a matching row', () => {
  const calls = sbs.match(/\{@html hitRows\.has\(line\.row\)\s*\? markMatchesInHtml\(line\.html \?\? '', searchQuery, findMarkClass\(line\.row === currentHitRow\)\)\s*: line\.html \?\? ''\}/g);
  assert.equal(calls?.length, 2, 'one per column');
});

test('the commit-history list marks its matching rows', () => {
  assert.match(history,
    /\{@html findLineHitSet\.has\(i\)\s*\? markDiffLine\(line\.html, line\.text, findQuery, findMarkClass\(i === currentFindLine\)\)\s*: line\.html\}/);
});

test('the marks are styled globally — {@html} content is out of scoped CSS\'s reach', () => {
  assert.match(style, /mark\.diff-find-mark \{[^}]*background:/);
  assert.match(style, /mark\.diff-find-mark\.current \{[^}]*background:/);
});
