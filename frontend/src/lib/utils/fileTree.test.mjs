/**
 * Tests for the diff file-tree builder.
 *
 * The project has no test framework and adding one wasn't wanted, so this runs
 * on Node's built-in test runner and transpiles the TypeScript module with the
 * esbuild that Vite already pulls in — no new dependency.
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
const dir = mkdtempSync(join(tmpdir(), 'asmgr-filetree-'));
const jsPath = join(dir, 'fileTree.mjs');
writeFileSync(
  jsPath,
  transformSync(readFileSync(join(here, 'fileTree.ts'), 'utf8'), { loader: 'ts', format: 'esm' }).code
);
const { buildFileTree, buildTreeRows } = await import(pathToFileURL(jsPath).href);
process.on('exit', () => rmSync(dir, { recursive: true, force: true }));

/** Shorthand: a file with the given path and trivial stats. */
const f = (path, added = 1, removed = 0) => ({ path, added, removed });

test('groups files by directory and nests them', () => {
  const root = buildFileTree([f('src/a.ts'), f('src/b.ts'), f('src/lib/c.ts')]);
  assert.equal(root.dirs.length, 1);
  const src = root.dirs[0];
  assert.equal(src.path, 'src');
  assert.equal(src.label, 'src');
  assert.deepEqual(src.files.map(x => x.path), ['src/a.ts', 'src/b.ts']);
  assert.equal(src.dirs.length, 1);
  assert.equal(src.dirs[0].path, 'src/lib');
});

test('collapses a chain of single-child directories into one row', () => {
  const root = buildFileTree([f('src/lib/components/Diff.svelte')]);
  assert.equal(root.dirs.length, 1);
  const node = root.dirs[0];
  assert.equal(node.label, 'src/lib/components');
  // The identity stays the DEEPEST path, so collapsing addresses the directory
  // whose contents are actually shown.
  assert.equal(node.path, 'src/lib/components');
  assert.deepEqual(node.files.map(x => x.path), ['src/lib/components/Diff.svelte']);
});

test('stops collapsing where a directory branches', () => {
  const root = buildFileTree([f('a/b/c/one.ts'), f('a/b/d/two.ts')]);
  const ab = root.dirs[0];
  assert.equal(ab.label, 'a/b');
  assert.deepEqual(ab.dirs.map(d => d.label), ['c', 'd']);
});

test('stops collapsing where a directory holds files of its own', () => {
  const root = buildFileTree([f('a/keep.ts'), f('a/b/nested.ts')]);
  const a = root.dirs[0];
  assert.equal(a.label, 'a');
  assert.deepEqual(a.files.map(x => x.path), ['a/keep.ts']);
  assert.deepEqual(a.dirs.map(d => d.label), ['b']);
});

test('sums added/removed and file counts over all descendants', () => {
  const root = buildFileTree([f('a/b/one.ts', 3, 1), f('a/c/two.ts', 5, 2), f('top.ts', 7, 4)]);
  const a = root.dirs[0];
  assert.equal(a.added, 8);
  assert.equal(a.removed, 3);
  assert.equal(a.fileCount, 2);
  assert.equal(root.added, 15);
  assert.equal(root.fileCount, 3);
});

test('keeps a repo-root file out of any directory', () => {
  const root = buildFileTree([f('README.md')]);
  assert.equal(root.dirs.length, 0);
  assert.deepEqual(root.files.map(x => x.path), ['README.md']);
});

test('handles a single deeply nested file', () => {
  const rows = buildTreeRows([f('a/b/c/d/e.ts')]);
  assert.deepEqual(
    rows.map(r => [r.kind, r.depth, r.kind === 'dir' ? r.label : r.path]),
    [['dir', 0, 'a/b/c/d'], ['file', 1, 'a/b/c/d/e.ts']]
  );
});

test('handles an empty file list', () => {
  const root = buildFileTree([]);
  assert.equal(root.dirs.length, 0);
  assert.equal(root.files.length, 0);
  assert.equal(root.fileCount, 0);
  assert.deepEqual(buildTreeRows([]), []);
});

test('tolerates unusual characters and doubled separators', () => {
  const root = buildFileTree([f('my dir/ünicode — file (1).ts'), f('a//b.ts'), f('c/')]);
  assert.deepEqual(root.dirs.map(d => d.label), ['my dir', 'a']);
  // The doubled separator must not create a nameless directory level.
  assert.deepEqual(root.dirs[1].files.map(x => x.path), ['a//b.ts']);
  // A path ending in a separator has no basename, so it contributes no file.
  assert.equal(root.fileCount, 2);
});

test('emits directories before files at the same level', () => {
  const rows = buildTreeRows([f('root.ts'), f('dir/inner.ts')]);
  assert.deepEqual(rows.map(r => r.kind), ['dir', 'file', 'file']);
  assert.equal(rows[1].path, 'dir/inner.ts');
  assert.equal(rows[2].path, 'root.ts');
});

test('a collapsed directory hides its descendants but keeps its own row', () => {
  const files = [f('src/a.ts'), f('src/lib/b.ts'), f('top.ts')];
  const all = buildTreeRows(files);
  assert.equal(all.length, 5); // src, src/lib, b.ts, a.ts, top.ts
  const folded = buildTreeRows(files, new Set(['src']));
  assert.deepEqual(folded.map(r => r.path), ['src', 'top.ts']);
});

test('collapsing an inner directory leaves its siblings visible', () => {
  const files = [f('a/b/one.ts'), f('a/c/two.ts')];
  const folded = buildTreeRows(files, new Set(['a/b']));
  assert.deepEqual(folded.map(r => r.path), ['a', 'a/b', 'a/c', 'a/c/two.ts']);
});
