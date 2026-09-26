import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * Find in every diff renderer ("I can't search in the whole-file diff").
 *
 * The bar lived in the side-by-side view alone, and a brand-new or deleted file
 * is drawn in one column even with two chosen — so for exactly those files it
 * was gone. The matching is now shared (utils/diffFind.ts) and every renderer
 * offers the bar. The unit tests cover the helper; the source checks cover the
 * wiring, which cannot be imported out of a .svelte file. The browser suite
 * (tests/browser/diff-find.spec.mjs) drives the real components.
 */

const root = new URL('..', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8').replace(/\r\n/g, '\n');

async function compile(path, name) {
  const dir = mkdtempSync(join(tmpdir(), 'diff-find-'));
  const js = join(dir, `${name}.mjs`);
  writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
    input: read(path),
    encoding: 'utf8',
    cwd: root.pathname,
  }));
  return import(js);
}

const find = await compile('src/lib/utils/diffFind.ts', 'diffFind');
const { parseDiff } = await compile('src/lib/utils/diffParse.ts', 'diffParse');
const { hasOneSide } = await compile('src/lib/utils/sideBySide.ts', 'sideBySide');

// --- the helper -------------------------------------------------------------

test('highlighted markup is matched as the text it shows', () => {
  const html = '<span style="color:#c678dd">if</span> (a &lt; b &amp;&amp; c &gt; d) &quot;x&quot; &#39;y&#x27;';
  assert.equal(find.htmlToText(html), 'if (a < b && c > d) "x" \'y\'');
  assert.equal(find.htmlToText(null), '');
  // An entity-looking run that is not one stays as typed.
  assert.equal(find.htmlToText('&bogus; &amp;lt;'), '&bogus; &lt;');
});

test('a search for "span" does not hit the colouring', () => {
  const texts = ['<span style="color:red">foo</span>'].map(find.htmlToText);
  assert.deepEqual(find.findMatches(texts, 'span'), []);
  assert.deepEqual(find.findMatches(texts, 'foo'), [0]);
});

test('the +/- marker is not part of what is searched', () => {
  assert.equal(find.diffLineText('+added'), 'added');
  assert.equal(find.diffLineText('-removed'), 'removed');
  assert.equal(find.diffLineText(' context'), 'context');
  assert.equal(find.diffLineText('@@ -1,2 +1,3 @@ fn'), '@@ -1,2 +1,3 @@ fn');
  assert.equal(find.diffLineText('--- a/x'), '--- a/x');
  assert.equal(find.diffLineText(''), '');
  const texts = ['+a', '-b', ' c'].map(find.diffLineText);
  assert.deepEqual(find.findMatches(texts, '-'), [], 'a "-" must not hit every removed line');
});

test('matching ignores case and a blank query matches nothing', () => {
  const texts = ['Needle', 'hay', 'NEEDLE here', 'needle'];
  assert.deepEqual(find.findMatches(texts, 'needle'), [0, 2, 3]);
  assert.deepEqual(find.findMatches(texts, 'NeEdLe'), [0, 2, 3]);
  assert.deepEqual(find.findMatches(texts, ''), []);
  assert.deepEqual(find.findMatches(texts, '   '), []);
});

test('stepping wraps, and from "on no match" lands on an end', () => {
  assert.equal(find.stepMatch(0, 1, 3), 1);
  assert.equal(find.stepMatch(2, 1, 3), 0);
  assert.equal(find.stepMatch(0, -1, 3), 2);
  assert.equal(find.stepMatch(-1, 1, 3), 0);
  assert.equal(find.stepMatch(-1, -1, 3), 2);
  assert.equal(find.stepMatch(0, 1, 0), -1);
});

test('a re-run keeps the cursor on its row only while that row still matches', () => {
  assert.equal(find.keepMatch([3, 9, 20], 9), 1);
  assert.equal(find.keepMatch([3, 20], 9), -1);
  assert.equal(find.keepMatch([3, 9], -1), -1);
});

test('the counter reads n/m, –/m off any match, and "no matches" only for a query', () => {
  assert.equal(find.matchCounter(0, 2, 'x', 'none'), '1/2');
  assert.equal(find.matchCounter(-1, 2, 'x', 'none'), '–/2');
  assert.equal(find.matchCounter(-1, 0, 'x', 'none'), 'none');
  assert.equal(find.matchCounter(-1, 0, '', 'none'), '');
});

test('the keys of the find field', () => {
  const press = (key, mods = {}) => find.findKeyAction({ key, ...mods });
  assert.equal(press('Escape'), 'close');
  assert.equal(press('Enter'), 1);
  assert.equal(press('Enter', { shiftKey: true }), -1);
  assert.equal(press('F3'), 1);
  assert.equal(press('F3', { shiftKey: true }), -1);
  assert.equal(press('g', { ctrlKey: true }), 1);
  assert.equal(press('g', { metaKey: true }), 1);
  assert.equal(press('ArrowDown'), 1);
  assert.equal(press('ArrowUp'), -1);
  assert.equal(press('g'), null, 'typing a g is typing');
  assert.equal(press('a'), null);
  assert.equal(press('ArrowLeft'), null, 'left and right still move the caret');
  assert.equal(press('ArrowRight'), null);
});

// --- a brand-new file, the case reported ------------------------------------

test('a new file with two columns chosen is drawn in one — and its lines are searchable', () => {
  const file = {
    status: 'added',
    hunks: [{ header: '@@ -0,0 +1,3 @@', body: '+first\n+const needle = 1;\n+last\n' }],
  };
  assert.equal(hasOneSide(file), true, 'the premise: one column even with two chosen');
  // What Diff.svelte's one-column search runs over: the parsed lines, markers dropped.
  const texts = parseDiff(file.hunks[0].body).map((line) => find.diffLineText(line.text));
  assert.deepEqual(find.findMatches(texts, 'NEEDLE'), [1]);
});

// --- wiring -----------------------------------------------------------------

const diff = read('src/lib/components/MainPanel/Diff.svelte');
const bar = read('src/lib/components/MainPanel/DiffFindBar.svelte');
const sbs = read('src/lib/components/MainPanel/SideBySideDiff.svelte');
const virtual = read('src/lib/components/MainPanel/VirtualLines.svelte');
const history = read('src/lib/components/Dialogs/GitHistoryDialog.svelte');

function fn(src, name) {
  const at = src.indexOf(`function ${name}(`);
  assert.ok(at >= 0, `${name} is missing`);
  const rest = src.slice(at);
  return rest.slice(0, rest.indexOf('\n  }\n') + 4);
}

test('the find button is offered in every view, not only in two columns', () => {
  const at = diff.indexOf("title=\"{$t('diff.findPlaceholder')} (Ctrl+F)\"");
  assert.ok(at >= 0, 'the find button is missing');
  // From the end of the button before it: anything gating this one sits there.
  const before = diff.slice(diff.lastIndexOf('</button>', at), at);
  assert.doesNotMatch(before, /\{#if sideBySide\}/, 'the find button is still gated on two columns');
});

test('Ctrl+F opens the bar in any renderer, only while this diff is active', () => {
  const body = fn(diff, 'handleDiffKeydown');
  assert.doesNotMatch(body, /sideBySide/, 'Ctrl+F is still limited to two columns');
  assert.match(body, /if \(!active\) return;/, 'a second, hidden diff would answer Ctrl+F too');
  assert.match(body, /openDiffFind\(\)/);
});

test('the bar sits above whichever renderer is showing', () => {
  const barAt = diff.indexOf('<DiffFindBar');
  const contentAt = diff.indexOf('<div class="diff-content"');
  assert.ok(barAt >= 0, 'Diff.svelte no longer renders the find bar');
  assert.ok(barAt < contentAt, 'the bar is inside one renderer\'s branch rather than above all of them');
  assert.match(diff.slice(barAt - 400, barAt), /\{#if showDiffFind && selectedFile\}/);
});

test('the search follows the renderer on screen, not the one chosen', () => {
  const body = fn(diff, 'runDiffSearch');
  assert.match(body, /if \(sideBySide\)/);
  assert.doesNotMatch(body, /sideBySideChosen/,
    'a new file with two columns chosen is drawn in one; searching by the choice would search the wrong view');
  assert.match(body, /findMatches\(findTexts\(wholeFileView\), diffQuery\)/);
  const texts = fn(diff, 'findTexts');
  assert.match(texts, /whole \? flatLines : unifiedFindLines/, 'the whole-file view must search its data, not its DOM');
  assert.match(texts, /diffLineText\(line\.text\)/);
});

test('whole-file matches are marked and reached by index in the virtual list', () => {
  assert.match(diff, /hitLines=\{lineHitSet\}/);
  assert.match(diff, /currentHit=\{currentLineHit\}/);
  assert.match(fn(diff, 'revealLineHit'), /virtualLines\?\.scrollToLine\(index\)/);
  assert.match(virtual, /export let hitLines/);
  assert.match(virtual, /class:hit=\{hitLines\.has\(first \+ i\)\}/);
  assert.match(virtual, /class:hit-current=\{first \+ i === currentHit\}/);
  assert.match(virtual, /\.diff-line\.hit-current \{/);
});

test('hunk-list matches are marked and scrolled to', () => {
  assert.match(diff, /class:hit=\{lineHitSet\.has\(hunkLineOffsets\[h\] \+ j\)\}/);
  assert.match(diff, /data-find=\{hunkLineOffsets\[h\] \+ j\}/);
  assert.match(fn(diff, 'revealLineHit'), /\[data-find="\$\{index\}"\]/);
  assert.match(diff, /\.diff-line\.hit-current \{/);
});

test('an open search is re-run when the file or the view changes', () => {
  assert.match(diff, /\$: void rerunDiffSearch\(showDiffFind, sideBySide, wholeFileView, flatLines, renderedHunks\);/);
  assert.match(fn(diff, 'rerunDiffSearch'), /runDiffSearch\(false\)/);
});

test('the side-by-side view matches through the shared helper', () => {
  assert.match(sbs, /from '..\/..\/utils\/diffFind'/);
  assert.match(sbs, /findMatches\(/);
  assert.match(sbs, /htmlToText\(row\.oldHtml\)/);
  assert.doesNotMatch(sbs, /function plainText/, 'the DOM-based copy of the matching is back');
  assert.match(sbs, /export function search\(query: string, reveal = true\)/);
});

test('the bar takes its keys from the shared rules', () => {
  assert.match(bar, /const action = findKeyAction\(event\);/);
  assert.match(bar, /if \(isolateKeys\) event\.stopPropagation\(\);/);
  assert.match(bar, /matchCounter\(hitAt, hitCount, query, \$t\('notes\.noMatches'\)\)/);
});

test('the commit-history diff has the same find', () => {
  assert.match(history, /<DiffFindBar[\s\S]*?isolateKeys=\{true\}/, 'Escape and the arrows would close the dialog / walk commits');
  const keydown = fn(history, 'onKeydown');
  assert.match(keydown, /\(e\.ctrlKey \|\| e\.metaKey\) && e\.key === 'f'[\s\S]*?openFind\(\)/);
  const run = fn(history, 'runFind');
  assert.match(run, /if \(sideBySide\)/);
  assert.match(run, /findMatches\(diffLines\.map\(\(line\) => diffLineText\(line\.text\)\), findQuery\)/);
  assert.match(history, /class:hit=\{findLineHitSet\.has\(i\)\}/);
  assert.match(history, /\$: void rerunFind\(showFind, sideBySide, diffLines, sideBySideHunks\);/);
});
