/**
 * The whole revert path, from a real git diff to a patch git accepts.
 *
 * Replays what the component does: whole-file diff -> parseDiff (which keeps
 * the +/- marker) -> strip the marker -> pair into rows -> pick a block ->
 * translate the row range back to hunk lines -> build the patch -> apply it.
 *
 * Every one of those steps is a place to be off by one, and only git can say.
 */
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const FE = new URL('..', import.meta.url).pathname;
const load = async (rel, name) => {
  const dir = mkdtempSync(join(tmpdir(), 'e-'));
  const out = join(dir, name);
  writeFileSync(out, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
    input: readFileSync(join(FE, rel), 'utf8'), encoding: 'utf8', cwd: FE,
  }));
  return import(out);
};

const { buildSideBySide, parseHunkHeader } = await load('src/lib/utils/sideBySide.ts', 'sbs.mjs');
const { buildBlockPatch } = await load('src/lib/utils/blockPatch.ts', 'bp.mjs');

// The component's parseDiff, verbatim: it KEEPS the leading marker.
function parseDiff(content) {
  return content.split('\n').map((line) => {
    let type = 'context';
    if (line.startsWith('+') && !line.startsWith('+++')) type = 'add';
    else if (line.startsWith('-') && !line.startsWith('---')) type = 'remove';
    else if (line.startsWith('@@')) type = 'header';
    else if (line.startsWith('diff ') || line.startsWith('index ') ||
             line.startsWith('+++') || line.startsWith('---')) type = 'meta';
    return { text: line, type };
  });
}

// The component's translation from paired rows back to hunk lines.
function hunkRange(paired, fromRow, toRow) {
  let at = 0, from = -1, to = -1;
  for (const [rowIndex, row] of paired.entries()) {
    const count = row.kind === 'change'
      ? (row.oldHtml !== null ? 1 : 0) + (row.newHtml !== null ? 1 : 0)
      : 1;
    if (rowIndex >= fromRow && rowIndex <= toRow && count > 0) {
      if (from === -1) from = at;
      to = at + count - 1;
    }
    at += count;
  }
  return from === -1 ? null : { from, to };
}

const numbered = (n) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n') + '\n';

function scenario(name, original, edited, blockIndex, expect) {
  const repo = mkdtempSync(join(tmpdir(), 'repo-'));
  const git = (...a) => execFileSync('git', ['-C', repo, ...a], { encoding: 'utf8' });
  git('init', '-q'); git('config', 'user.email', 't@t'); git('config', 'user.name', 't'); git('config', 'commit.gpgsign', 'false');
  writeFileSync(join(repo, 'f.txt'), original);
  git('add', '.'); git('commit', '-qm', 'base');
  writeFileSync(join(repo, 'f.txt'), edited);

  // What the backend hands the whole-file view: the hunk body, marker and all.
  const raw = git('diff', '-U100000', '--', 'f.txt');
  const lines = raw.split('\n');
  const at = lines.findIndex((l) => l.startsWith('@@'));
  const header = lines[at];
  const body = lines.slice(at + 1).filter((l) => l.length && !l.startsWith('\\')).join('\n');

  // The component's chain.
  const parsed = parseDiff(body);
  const stripped = parsed.map((l) => ({
    type: l.type,
    text: ['add', 'remove', 'context'].includes(l.type) ? l.text.slice(1) : l.text,
  }));
  const forColumns = parsed.map((l) => ({ type: l.type, html: l.text }));
  const paired = buildSideBySide(header, forColumns);

  const runs = [];
  paired.forEach((row, i) => {
    if (row.kind !== 'change') return;
    const last = runs[runs.length - 1];
    if (last && last.to === i - 1) last.to = i;
    else runs.push({ from: i, to: i });
  });

  const block = runs[blockIndex];
  const range = hunkRange(paired, block.from, block.to);
  const { oldStart, newStart } = parseHunkHeader(header);
  const patch = buildBlockPatch('f.txt', null, stripped, oldStart, newStart, range.from, range.to);

  try {
    execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch, stdio: ['pipe', 'pipe', 'pipe'] });
  } catch (e) {
    console.log(`${name}: git REJECTED the patch — ${String(e.stderr).trim()}`);
    console.log(patch);
    return false;
  }

  const after = readFileSync(join(repo, 'f.txt'), 'utf8');
  const ok = expect(after);
  if (!ok) console.log(`${name}: WRONG RESULT`);
  if (!ok) console.log(after);
  return ok;
}

let all = true;
all &= scenario(
  'two changes, revert the first ',
  numbered(30),
  numbered(30).replace('line 5\n', 'X five\n').replace('line 25\n', 'X twentyfive\n'),
  0,
  (s) => /^line 5$/m.test(s) && /X twentyfive/.test(s),
);
all &= scenario(
  'two changes, revert the second',
  numbered(30),
  numbered(30).replace('line 5\n', 'X five\n').replace('line 25\n', 'X twentyfive\n'),
  1,
  (s) => /X five/.test(s) && /^line 25$/m.test(s),
);
all &= scenario(
  'three-for-two replacement    ',
  numbered(20),
  numbered(20).replace('line 8\nline 9\nline 10\n', 'A\nB\n'),
  0,
  (s) => s === numbered(20),
);
all &= scenario(
  'pure insertion               ',
  numbered(12),
  numbered(12).replace('line 6\n', 'line 6\nNEW1\nNEW2\nNEW3\n'),
  0,
  (s) => s === numbered(12),
);
all &= scenario(
  'pure deletion                ',
  numbered(12),
  numbered(12).replace('line 3\nline 4\n', ''),
  0,
  (s) => s === numbered(12),
);

import assert from 'node:assert/strict';
assert.ok(all, 'a block revert produced a patch git rejected or a wrong result');
console.log('blockRevert: ok');
