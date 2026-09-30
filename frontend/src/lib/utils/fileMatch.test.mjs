/**
 * Tests for the quick-open ranking.
 *
 * Same arrangement as fileTree.test.mjs: Node's built-in runner, with the
 * TypeScript transpiled by the esbuild that Vite already pulls in — no new
 * dependency.
 *
 *   cd frontend && npm test
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { transformSync } from 'esbuild';

const here = dirname(fileURLToPath(import.meta.url));
const dir = mkdtempSync(join(tmpdir(), 'asmgr-filematch-'));
const jsPath = join(dir, 'fileMatch.mjs');
writeFileSync(
  jsPath,
  transformSync(readFileSync(join(here, 'fileMatch.ts'), 'utf8'), { loader: 'ts', format: 'esm' }).code
);
const { scorePath, rankFiles, highlightSegments } = await import(pathToFileURL(jsPath).href);
process.on('exit', () => rmSync(dir, { recursive: true, force: true }));

/** Shorthand: an index candidate built from its path. */
const c = (path) => ({ path, name: path.slice(path.lastIndexOf('/') + 1) });
/** Rank and return just the paths, best first. */
const order = (paths, query, limit) => rankFiles(paths.map(c), query, limit).map((m) => m.path);

test('matches a subsequence, not just a substring', () => {
  const m = scorePath('src/lib/components/MainPanel/FileBrowser.svelte', 'FileBrowser.svelte', 'flbrsv');
  assert.ok(m, 'initials scattered through the path must match');
  assert.equal(scorePath('src/main.go', 'main.go', 'xyz'), null);
});

test('a path missing one query character does not match at all', () => {
  assert.equal(scorePath('src/app.ts', 'app.ts', 'apq'), null);
});

test('an empty query matches everything with no highlight', () => {
  const m = scorePath('a/b.ts', 'b.ts', '');
  assert.deepEqual(m.positions, []);
  assert.equal(m.score, 0);
});

test('prefers a match in the basename over the directory', () => {
  const ranked = order(['browser/notes.txt', 'docs/browser.md'], 'browser');
  assert.equal(ranked[0], 'docs/browser.md');
});

test('prefers consecutive characters over scattered ones', () => {
  const ranked = order(['src/index.ts', 'i/n/d/e/x.ts'], 'index');
  assert.equal(ranked[0], 'src/index.ts');
});

test('prefers the shorter path when the match is otherwise equal', () => {
  const ranked = order(['a/util.ts', 'a/b/c/d/e/util.ts'], 'util');
  assert.equal(ranked[0], 'a/util.ts');
});

test('prefers a match starting the basename', () => {
  const ranked = order(['src/mybrowser.ts', 'src/browser.ts'], 'browser');
  assert.equal(ranked[0], 'src/browser.ts');
});

test('rewards word boundaries, so initials find a camelCase name', () => {
  const ranked = order(['src/FileBrowser.svelte', 'src/affluentbeaver.txt'], 'fb');
  assert.equal(ranked[0], 'src/FileBrowser.svelte');
});

test('separator-delimited initials rank above an incidental subsequence', () => {
  const ranked = order(['src/lib/util.ts', 'source/xxlxxixxbxx/uuu.ts'], 'slu');
  assert.equal(ranked[0], 'src/lib/util.ts');
});

test('is case-insensitive in both directions', () => {
  assert.ok(scorePath('src/FileBrowser.svelte', 'FileBrowser.svelte', 'filebrowser'));
  const ranked = order(['src/FileBrowser.svelte'], 'FILEBROWSER');
  assert.deepEqual(ranked, ['src/FileBrowser.svelte']);
});

test('a space in the query separates words rather than being matched', () => {
  const m = scorePath('src/FileBrowser.svelte', 'FileBrowser.svelte', 'file browser');
  assert.ok(m, 'no source path contains a space, so the query space must be skipped');
});

test('an empty query lists the shortest paths first', () => {
  const ranked = order(['a/b/c/deep.ts', 'top.ts', 'a/mid.ts'], '');
  assert.deepEqual(ranked, ['top.ts', 'a/mid.ts', 'a/b/c/deep.ts']);
});

test('the limit caps the result list', () => {
  const paths = Array.from({ length: 50 }, (_, i) => `src/file${i}.ts`);
  assert.equal(order(paths, 'file', 10).length, 10);
  assert.equal(order(paths, '', 7).length, 7);
});

test('the order is deterministic for equally scored paths', () => {
  const paths = ['b/x.ts', 'a/x.ts', 'c/x.ts'];
  assert.deepEqual(order(paths, 'x.ts'), order(paths.slice().reverse(), 'x.ts'));
});

test('positions point at the matched characters', () => {
  const m = scorePath('src/app.ts', 'app.ts', 'app');
  assert.deepEqual(
    m.positions.map((i) => 'src/app.ts'[i]).join(''),
    'app'
  );
});

test('highlightSegments merges adjacent matches into one run', () => {
  const m = scorePath('src/app.ts', 'app.ts', 'app');
  const segments = highlightSegments('src/app.ts', m.positions);
  assert.deepEqual(segments, [
    { text: 'src/', matched: false },
    { text: 'app', matched: true },
    { text: '.ts', matched: false },
  ]);
});

test('highlightSegments round-trips the whole path', () => {
  const path = 'src/lib/components/FileBrowser.svelte';
  const m = scorePath(path, 'FileBrowser.svelte', 'flbrsv');
  const segments = highlightSegments(path, m.positions);
  assert.equal(segments.map((s) => s.text).join(''), path);
  assert.ok(segments.some((s) => s.matched));
});

test('highlightSegments with no positions returns the path unmarked', () => {
  assert.deepEqual(highlightSegments('a/b.ts', []), [{ text: 'a/b.ts', matched: false }]);
});

test('a match at the very start of the path yields no leading empty segment', () => {
  const m = scorePath('app.ts', 'app.ts', 'app');
  const segments = highlightSegments('app.ts', m.positions);
  assert.equal(segments[0].text, 'app');
  assert.equal(segments[0].matched, true);
});

// "release" brought up files that only held its letters scattered in order,
// and a file named release-notes.md could rank low, its letters taken from the
// directory names on the way. Whole pieces of the query come first now, and
// scattered letters only when nothing holds the query whole.
const releaseIndex = [
  c('frontend/src/lib/components/Sidebar/SessionList.svelte'),
  c('scripts/release-notes.md'),
  c('docs/RELEASE.md'),
  c('frontend/src/lib/utils/releaseCheck.ts'),
  c('frontend/src/lib/stores/sessions.ts'),
];

test('drops files that only hold the letters scattered', () => {
  const paths = rankFiles(releaseIndex, 'release').map((m) => m.path);
  assert.deepEqual(paths.sort(), ['docs/RELEASE.md', 'frontend/src/lib/utils/releaseCheck.ts', 'scripts/release-notes.md']);
});

test('marks the whole word, where it is in the file name', () => {
  const [hit] = rankFiles([c('release/tools/release-notes.md')], 'release');
  assert.deepEqual(hit.positions, [14, 15, 16, 17, 18, 19, 20]);
});

test('ranks a file name starting with the query first', () => {
  assert.equal(rankFiles(releaseIndex, 'release')[0].path, 'docs/RELEASE.md');
});

test('finds each word of a query somewhere, in any order', () => {
  const paths = rankFiles(releaseIndex, 'check release').map((m) => m.path);
  assert.deepEqual(paths, ['frontend/src/lib/utils/releaseCheck.ts']);
});

test('still finds a file by its initials when nothing holds the query whole', () => {
  const paths = rankFiles([c('frontend/src/lib/components/MainPanel/FileBrowser.svelte'), c('README.md')], 'flbrsv')
    .map((m) => m.path);
  assert.deepEqual(paths, ['frontend/src/lib/components/MainPanel/FileBrowser.svelte']);
});
