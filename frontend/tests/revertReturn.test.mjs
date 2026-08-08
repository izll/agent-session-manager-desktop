/**
 * After reverting a block, does the view come back to the right place?
 *
 * Two things have to line up: the line recorded BEFORE the revert (computed
 * from the old diff) and where that line sits in the NEW diff, which has fewer
 * lines. Both are arithmetic over diff lines, and both are easy to get one off.
 */
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const FE = new URL('..', import.meta.url).pathname;
const load = async (rel, name) => {
  const d = mkdtempSync(join(tmpdir(), 'r-'));
  const o = join(d, name);
  writeFileSync(o, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
    input: readFileSync(join(FE, rel), 'utf8'), encoding: 'utf8', cwd: FE,
  }));
  return import(o);
};
const { parseHunkHeader } = await load('src/lib/utils/sideBySide.ts', 's.mjs');
const { buildBlockPatch } = await load('src/lib/utils/blockPatch.ts', 'b.mjs');

const parseDiff = (content) => content.split('\n').map((line) => {
  let type = 'context';
  if (line.startsWith('+') && !line.startsWith('+++')) type = 'add';
  else if (line.startsWith('-') && !line.startsWith('---')) type = 'remove';
  else if (line.startsWith('@@')) type = 'header';
  else if (line.startsWith('diff ') || line.startsWith('index ') ||
           line.startsWith('+++') || line.startsWith('---')) type = 'meta';
  return { text: line, type };
});

// The component's unifiedPositionOf, over flatLines built the same way.
function unifiedPositionOf(flatLines, fileLine) {
  let number = 0;
  for (let at = 0; at < flatLines.length; at++) {
    const type = flatLines[at].type;
    if (type === 'header' || type === 'meta') {
      if (type === 'header') number = parseHunkHeader(flatLines[at].html.replace(/<[^>]*>/g, '')).oldStart - 1;
      continue;
    }
    if (type === 'add') continue;
    number++;
    if (number >= fileLine) return at;
  }
  return -1;
}

const numbered = (n) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n') + '\n';

const repo = mkdtempSync(join(tmpdir(), 'repo-'));
const git = (...a) => execFileSync('git', ['-C', repo, ...a], { encoding: 'utf8' });
git('init', '-q'); git('config', 'user.email', 't@t'); git('config', 'user.name', 't'); git('config', 'commit.gpgsign', 'false');
writeFileSync(join(repo, 'f.txt'), numbered(60));
git('add', '.'); git('commit', '-qm', 'base');

// A change well down the file, plus one above it that must survive.
writeFileSync(join(repo, 'f.txt'), numbered(60)
  .replace('line 10\n', 'CHANGED ten\n')
  .replace('line 40\n', 'CHANGED forty\nEXTRA\n'));

function hunkOf() {
  const raw = git('diff', '-U100000', '--', 'f.txt');
  const all = raw.split('\n');
  const at = all.findIndex((l) => l.startsWith('@@'));
  return { header: all[at], body: all.slice(at + 1).filter((l) => l.length && !l.startsWith('\\')).join('\n') };
}

const hunk = hunkOf();
const lines = parseDiff(hunk.body);
const { oldStart, newStart } = parseHunkHeader(hunk.header);

// The second block (around line 40).
const runs = [];
lines.forEach((l, i) => {
  if (l.type !== 'add' && l.type !== 'remove') return;
  const last = runs[runs.length - 1];
  if (last && last.to === i - 1) last.to = i;
  else runs.push({ from: i, to: i });
});
const block = runs[1];

// The component's return line, computed before the revert: where the block
// starts in the OLD file, which is what reverting restores.
let blockLine = oldStart;
for (let at = 0; at < block.from; at++) {
  if (lines[at].type === 'remove' || lines[at].type === 'context') blockLine++;
}
const returnToLine = Math.max(1, blockLine);

// Apply the revert for real.
const patch = buildBlockPatch('f.txt', null, lines.map((l) => ({
  type: l.type,
  text: ['add', 'remove', 'context'].includes(l.type) ? l.text.slice(1) : l.text,
})), oldStart, newStart, block.from, block.to);
execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

const after = readFileSync(join(repo, 'f.txt'), 'utf8').split('\n');

// And where the unified view would scroll to.
const hunk2 = hunkOf();
const flat = parseDiff(hunk2.body).map((l) => ({ type: l.type, html: l.text }));
const at = unifiedPositionOf(flat, returnToLine);

import assert from 'node:assert/strict';

// The line recorded before the revert must still be that line afterwards —
// the block's own lines are gone, so anything inside it would have moved.
// The block was "line 40" replaced by two lines; reverting restores it, and the
// recorded line is where that change was — not one above it. Landing a line
// early and then scrolling a third of the way down put the view consistently
// higher than the place being worked on.
assert.equal(after[returnToLine - 1], 'line 40',
  'the recorded line is where the block was, in the restored file');
// And the unified renderers, which count diff lines rather than file lines,
// have to find it too.
assert.match(flat[at]?.html ?? '', /line 40/,
  'the unified view must scroll to the same place');
// The block above must be untouched: reverting one block is the whole point.
assert.ok(after.includes('CHANGED ten'), 'the other change must survive');

console.log('revertReturn: ok');
