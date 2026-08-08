import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * Reverting ONE block of a whole-file diff.
 *
 * The revert sends a patch to `git apply --reverse`. A hunk's own patch is the
 * right thing when a hunk is one change, but in whole-file view git returns the
 * file as a single hunk (-U100000), so that patch covers every change in the
 * file — reverting one block would throw away all of them.
 *
 * So the patch is rebuilt around the block. The only test that means anything
 * here is whether git accepts it and leaves everything else alone, so this runs
 * real git against a real repository rather than comparing strings.
 */
const source = readFileSync(new URL('../src/lib/utils/blockPatch.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'bp-'));
const js = join(dir, 'blockPatch.mjs');
writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
  input: source,
  encoding: 'utf8',
  cwd: new URL('..', import.meta.url).pathname,
}));
const { buildBlockPatch } = await import(js);

/** A throwaway repository with one committed file. */
function repoWith(original) {
  const repo = mkdtempSync(join(tmpdir(), 'repo-'));
  const git = (...args) => execFileSync('git', ['-C', repo, ...args], { encoding: 'utf8' });
  git('init', '-q');
  git('config', 'user.email', 't@t');
  git('config', 'user.name', 't');
  git('config', 'commit.gpgsign', 'false');
  writeFileSync(join(repo, 'f.txt'), original);
  git('add', '.');
  git('commit', '-qm', 'base');
  return { repo, git };
}

/** The whole file as one hunk, the way the whole-file view asks for it. */
function wholeFileHunk(repo) {
  const out = execFileSync('git', ['-C', repo, 'diff', '-U100000', '--', 'f.txt'], { encoding: 'utf8' });
  const lines = out.split('\n');
  const at = lines.findIndex((l) => l.startsWith('@@'));
  assert.notEqual(at, -1, 'no hunk in the diff');
  const header = lines[at].match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
  const body = lines.slice(at + 1).filter((l) => l !== '' || false);
  return {
    oldStart: Number(header[1]),
    newStart: Number(header[2]),
    lines: body
      .filter((l) => l.length > 0 && !l.startsWith('\\'))
      .map((l) => ({
        type: l[0] === '+' ? 'add' : l[0] === '-' ? 'remove' : 'context',
        text: l.slice(1),
      })),
  };
}

/** Where each run of changed lines starts and ends. */
function blocks(lines) {
  const runs = [];
  lines.forEach((line, at) => {
    if (line.type === 'context') return;
    const last = runs[runs.length - 1];
    if (last && last.to === at - 1) last.to = at;
    else runs.push({ from: at, to: at });
  });
  return runs;
}

const numbered = (n) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n') + '\n';

// Two separate changes in one file. Reverting the first must restore it and
// leave the second exactly as it was — the failure this whole file exists for.
{
  const { repo, git } = repoWith(numbered(30));
  const edited = numbered(30)
    .replace('line 5\n', 'CHANGED five\n')
    .replace('line 25\n', 'CHANGED twenty-five\n');
  writeFileSync(join(repo, 'f.txt'), edited);

  const hunk = wholeFileHunk(repo);
  const runs = blocks(hunk.lines);
  assert.equal(runs.length, 2, 'the file should hold two separate changes');

  const patch = buildBlockPatch('f.txt', null, hunk.lines, hunk.oldStart, hunk.newStart, runs[0].from, runs[0].to);
  assert.ok(patch, 'a block with changes must produce a patch');

  // The real test: git accepts it.
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

  const after = readFileSync(join(repo, 'f.txt'), 'utf8');
  assert.match(after, /^line 5$/m, 'the reverted block should be back to the original');
  assert.doesNotMatch(after, /CHANGED five/, 'the reverted change should be gone');
  assert.match(after, /CHANGED twenty-five/, 'the OTHER change must be left alone');
}

// And the second block, to be sure the arithmetic is not accidentally right
// only at the top of the file.
{
  const { repo } = repoWith(numbered(30));
  writeFileSync(join(repo, 'f.txt'), numbered(30)
    .replace('line 5\n', 'CHANGED five\n')
    .replace('line 25\n', 'CHANGED twenty-five\n'));

  const hunk = wholeFileHunk(repo);
  const runs = blocks(hunk.lines);
  const patch = buildBlockPatch('f.txt', null, hunk.lines, hunk.oldStart, hunk.newStart, runs[1].from, runs[1].to);
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

  const after = readFileSync(join(repo, 'f.txt'), 'utf8');
  assert.match(after, /CHANGED five/, 'the first change must survive');
  assert.match(after, /^line 25$/m, 'the second should be back');
}

// A pure insertion: nothing removed, so the old side of the header has a run of
// zero lines and git wants the line BEFORE the range as its start.
{
  const { repo } = repoWith(numbered(10));
  writeFileSync(join(repo, 'f.txt'), numbered(10).replace('line 5\n', 'line 5\nINSERTED a\nINSERTED b\n'));

  const hunk = wholeFileHunk(repo);
  const runs = blocks(hunk.lines);
  const patch = buildBlockPatch('f.txt', null, hunk.lines, hunk.oldStart, hunk.newStart, runs[0].from, runs[0].to);
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

  assert.equal(readFileSync(join(repo, 'f.txt'), 'utf8'), numbered(10), 'the insertion should be gone');
}

// A pure deletion, the mirror of the above.
{
  const { repo } = repoWith(numbered(10));
  writeFileSync(join(repo, 'f.txt'), numbered(10).replace('line 4\nline 5\n', ''));

  const hunk = wholeFileHunk(repo);
  const runs = blocks(hunk.lines);
  const patch = buildBlockPatch('f.txt', null, hunk.lines, hunk.oldStart, hunk.newStart, runs[0].from, runs[0].to);
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

  assert.equal(readFileSync(join(repo, 'f.txt'), 'utf8'), numbered(10), 'the deleted lines should be back');
}

// A change at the very first line, where there is no context above it to take.
{
  const { repo } = repoWith(numbered(10));
  writeFileSync(join(repo, 'f.txt'), numbered(10).replace('line 1\n', 'CHANGED one\n'));

  const hunk = wholeFileHunk(repo);
  const runs = blocks(hunk.lines);
  const patch = buildBlockPatch('f.txt', null, hunk.lines, hunk.oldStart, hunk.newStart, runs[0].from, runs[0].to);
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

  assert.equal(readFileSync(join(repo, 'f.txt'), 'utf8'), numbered(10), 'a change at the top must revert');
}

// And at the very last line, where there is none below.
{
  const { repo } = repoWith(numbered(10));
  writeFileSync(join(repo, 'f.txt'), numbered(10).replace('line 10\n', 'CHANGED ten\n'));

  const hunk = wholeFileHunk(repo);
  const runs = blocks(hunk.lines);
  const patch = buildBlockPatch('f.txt', null, hunk.lines, hunk.oldStart, hunk.newStart, runs[0].from, runs[0].to);
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

  assert.equal(readFileSync(join(repo, 'f.txt'), 'utf8'), numbered(10), 'a change at the bottom must revert');
}

// Three adjacent blocks separated by a single context line — the context
// windows overlap, which is where a naive cut takes a neighbour's change with
// it.
{
  const { repo } = repoWith(numbered(20));
  writeFileSync(join(repo, 'f.txt'), numbered(20)
    .replace('line 8\n', 'CHANGED eight\n')
    .replace('line 10\n', 'CHANGED ten\n')
    .replace('line 12\n', 'CHANGED twelve\n'));

  const hunk = wholeFileHunk(repo);
  const runs = blocks(hunk.lines);
  assert.equal(runs.length, 3, 'three changes, one context line apart');

  const patch = buildBlockPatch('f.txt', null, hunk.lines, hunk.oldStart, hunk.newStart, runs[1].from, runs[1].to);
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });

  const after = readFileSync(join(repo, 'f.txt'), 'utf8');
  assert.match(after, /CHANGED eight/, 'the block above must survive');
  assert.match(after, /^line 10$/m, 'the middle block should be back');
  assert.match(after, /CHANGED twelve/, 'the block below must survive');
}

// Nothing to reverse is not a no-op, it is an error — an empty patch would be
// rejected by git with a message that explains nothing.
{
  const lines = [
    { type: 'context', text: 'a' },
    { type: 'context', text: 'b' },
  ];
  assert.equal(
    buildBlockPatch('f.txt', null, lines, 1, 1, 0, 1),
    null,
    'a range with no change must produce no patch',
  );
  assert.equal(buildBlockPatch('f.txt', null, lines, 1, 1, 5, 9), null, 'an out-of-range block is not a patch');
}

// A renamed file needs its old name on the a/ side, or the patch cannot be
// located in the working tree.
{
  const patch = buildBlockPatch(
    'new/name.txt',
    'old/name.txt',
    [{ type: 'remove', text: 'x' }, { type: 'add', text: 'y' }],
    1,
    1,
    0,
    1,
  );
  assert.match(patch, /--- a\/old\/name\.txt/, 'the old name belongs on the a/ side');
  assert.match(patch, /\+\+\+ b\/new\/name\.txt/, 'the new name on the b/ side');
}

console.log('blockPatch: ok');

/**
 * A block at the very end of a file, fed the way the component feeds it.
 *
 * The backend appends a newline to EVERY line of a hunk body, including the
 * last, and the component splits that on newlines — so the array always ends
 * with an empty entry, sometimes two, since the diff itself ends with a blank
 * line. Those parse as context, being neither + nor -, and as context BELOW a
 * block at the end of the file they were written into the patch as empty lines
 * the file does not have. git refused the whole thing:
 *
 *   error: patch failed: README.md:242
 *   error: README.md: patch does not apply
 *
 * Reverting anything else in the file worked, which is why this survived the
 * earlier tests: they built the body with join(), and only a block at the very
 * end has that junk as its trailing context.
 */
{
  const { repo } = repoWith(numbered(10));
  writeFileSync(join(repo, 'f.txt'), numbered(10) + 'APPENDED a\nAPPENDED b\n');

  // Reproduce the body exactly: a newline after every line, then split.
  const out = execFileSync('git', ['-C', repo, 'diff', '-U100000', '--', 'f.txt'], { encoding: 'utf8' });
  const all = out.split('\n');
  const at = all.findIndex((l) => l.startsWith('@@'));
  const header = all[at].match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
  let body = '';
  for (const l of all.slice(at + 1)) body += l + '\n';

  const lines = body.split('\n').map((l) => ({
    type: l[0] === '+' ? 'add' : l[0] === '-' ? 'remove' : 'context',
    text: l.length ? l.slice(1) : '',
  }));
  assert.equal(lines[lines.length - 1].text, '', 'the split really does leave an empty entry');

  const runs = blocks(lines);
  const last = runs[runs.length - 1];

  const patch = buildBlockPatch('f.txt', null, lines, Number(header[1]), Number(header[2]), last.from, last.to);
  assert.ok(patch, 'a block at the end of the file must still produce a patch');
  assert.doesNotMatch(
    patch,
    /\n \n?$/,
    'the patch must not end with context lines the file does not have',
  );

  // The only verdict that counts.
  execFileSync('git', ['-C', repo, 'apply', '--reverse', '-'], { input: patch });
  assert.equal(
    readFileSync(join(repo, 'f.txt'), 'utf8'),
    numbered(10),
    'the appended lines should be gone and nothing else touched',
  );
}

console.log('blockPatch end-of-file: ok');
