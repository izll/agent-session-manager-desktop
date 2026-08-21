import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * Resuming the diff view after a tab switch.
 *
 * The Diff component is destroyed when the tab changes — the mount points are
 * in exclusive branches — so its scroll position and the change being stepped
 * through go with it, and coming back landed at the top of the file. In a long
 * review that means finding your place again after every glance at the
 * terminal.
 */
const source = readFileSync(new URL('../src/lib/utils/diffViewState.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'dvs-'));
const js = join(dir, 'diffViewState.mjs');
writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
  input: source,
  encoding: 'utf8',
  cwd: new URL('..', import.meta.url).pathname,
}));

const { rememberPlace, recallPlace, forgetSession, noteListKey } = await import(js);

const place = (scrollTop, currentHunk = 2, markedHunk = 2) => ({ scrollTop, currentHunk, markedHunk });

// The round trip: what was left is what comes back.
{
  rememberPlace('s1', 'src/app.ts', 'whole', place(1234, 3, 3));
  assert.deepEqual(recallPlace('s1', 'src/app.ts', 'whole'), place(1234, 3, 3));
}

// A file never opened has no place, so it starts at the top rather than
// inheriting someone else's offset.
{
  assert.equal(recallPlace('s1', 'src/never-opened.ts', 'whole'), null);
}

// Two sessions reviewed in turn keep their own places. Sharing them would drop
// you into the middle of a file you had not been reading.
{
  rememberPlace('s1', 'shared.ts', 'whole', place(100));
  rememberPlace('s2', 'shared.ts', 'whole', place(900));
  assert.equal(recallPlace('s1', 'shared.ts', 'whole').scrollTop, 100);
  assert.equal(recallPlace('s2', 'shared.ts', 'whole').scrollTop, 900);
}

// Session ids are only unique inside a project. Even when both projects point
// at the same canonical checkout, their UI history must remain independent.
{
  rememberPlace('same', 'shared.ts', 'whole', place(111), 0, '/repo', 'project-a');
  rememberPlace('same', 'shared.ts', 'whole', place(999), 0, '/repo', 'project-b');
  assert.equal(recallPlace('same', 'shared.ts', 'whole', 0, '/repo', 'project-a').scrollTop, 111);
  assert.equal(recallPlace('same', 'shared.ts', 'whole', 0, '/repo', 'project-b').scrollTop, 999);
}

// The three renderers lay the same file out differently — the columns pair
// lines up, the whole-file view holds every line, the hunk list only the
// changes — so an offset from one means nothing in another.
{
  rememberPlace('s1', 'modes.ts', 'whole', place(500));
  rememberPlace('s1', 'modes.ts', 'sbs', place(80));
  assert.equal(recallPlace('s1', 'modes.ts', 'whole').scrollTop, 500);
  assert.equal(recallPlace('s1', 'modes.ts', 'sbs').scrollTop, 80);
  assert.equal(recallPlace('s1', 'modes.ts', 'hunks'), null);
}

// The key is built from three parts, and a session id or path could otherwise
// spell another triple's key by running the parts together.
{
  rememberPlace('a', 'b', 'whole', place(1));
  rememberPlace('a\x1fwhole\x1fb', '', 'whole', place(2));   // ignored: no path
  assert.equal(recallPlace('a', 'b', 'whole').scrollTop, 1, 'the key must not be ambiguous');

  // The parts cannot be reordered into each other either.
  rememberPlace('x', 'y/z', 'whole', place(10));
  rememberPlace('x\x1fy', 'z', 'whole', place(20));
  assert.equal(recallPlace('x', 'y/z', 'whole').scrollTop, 10);
  assert.equal(recallPlace('x\x1fy', 'z', 'whole').scrollTop, 20);
}

// Nothing is stored without both a session and a file: an empty key would be
// handed back to whichever file asked next.
{
  rememberPlace('', 'file.ts', 'whole', place(50));
  rememberPlace('s3', '', 'whole', place(50));
  assert.equal(recallPlace('', 'file.ts', 'whole'), null);
  assert.equal(recallPlace('s3', '', 'whole'), null);
}

// When the diff is reloaded the file may have changed underneath, and an offset
// into the version just replaced would land somewhere arbitrary.
{
  rememberPlace('s4', 'a.ts', 'whole', place(700));
  rememberPlace('s4', 'b.ts', 'sbs', place(300));
  rememberPlace('s5', 'a.ts', 'whole', place(400));

  forgetSession('s4');

  assert.equal(recallPlace('s4', 'a.ts', 'whole'), null, 'every mode of the session goes');
  assert.equal(recallPlace('s4', 'b.ts', 'sbs'), null);
  assert.equal(recallPlace('s5', 'a.ts', 'whole').scrollTop, 400, 'other sessions are untouched');
}

// Deleting while iterating the live key set skips entries. A session with more
// than a couple of files is where that shows.
{
  for (let at = 0; at < 20; at++) rememberPlace('s6', `file${at}.ts`, 'whole', place(at + 1));
  forgetSession('s6');
  for (let at = 0; at < 20; at++) {
    assert.equal(recallPlace('s6', `file${at}.ts`, 'whole'), null, `file${at} survived the sweep`);
  }
}

// A session id that is a prefix of another must not be swept with it.
{
  rememberPlace('sess', 'a.ts', 'whole', place(11));
  rememberPlace('sess-2', 'a.ts', 'whole', place(22));
  forgetSession('sess');
  assert.equal(recallPlace('sess', 'a.ts', 'whole'), null);
  assert.equal(
    recallPlace('sess-2', 'a.ts', 'whole')?.scrollTop,
    22,
    'a longer session id starting with the same text must survive',
  );
}

/**
 * The shape of the diff, recorded here for the same reason the places are.
 *
 * This is the bug that made the resume never work: the component compared the
 * file list against a copy of the key held inside itself, and a tab switch
 * destroys the component. Coming back it was empty, so the very first load
 * looked like a changed list and forgot the places it was about to restore.
 */
{
  // The first sighting is a change: there was nothing to compare against.
  assert.equal(noteListKey('s7', 'a.ts:1:0'), true);
  rememberPlace('s7', 'a.ts', 'whole', place(640));

  // Seeing the SAME list again is not a change — this is the tab switch, and
  // the place has to survive it.
  assert.equal(noteListKey('s7', 'a.ts:1:0'), false, 'an unchanged list is not a change');
  assert.equal(
    recallPlace('s7', 'a.ts', 'whole')?.scrollTop,
    640,
    'switching away and back must not forget where the review was',
  );

  // A genuinely changed list forgets, because an offset into a file that has
  // been rewritten means nothing.
  assert.equal(noteListKey('s7', 'a.ts:9:2'), true, 'a different list is a change');
  assert.equal(recallPlace('s7', 'a.ts', 'whole'), null, 'stale contents mean stale offsets');
}

// Each session has its own diff, so one changing must not forget another's.
{
  noteListKey('s8', 'x:1:0');
  noteListKey('s9', 'y:1:0');
  rememberPlace('s8', 'x', 'whole', place(11));
  rememberPlace('s9', 'y', 'whole', place(22));

  assert.equal(noteListKey('s9', 'y:5:5'), true);
  assert.equal(recallPlace('s8', 'x', 'whole')?.scrollTop, 11, 'another session is untouched');
  assert.equal(recallPlace('s9', 'y', 'whole'), null);
}

// Two tabs of one session may point at different repositories. Their identical
// paths and list shapes must not share either content identity or scroll state.
{
  noteListKey('tabs', 'same.ts:1:0', 1, 'full');
  noteListKey('tabs', 'same.ts:1:0', 2, 'full');
  rememberPlace('tabs', 'same.ts', 'whole', place(111), 1);
  rememberPlace('tabs', 'same.ts', 'whole', place(222), 2);

  assert.equal(recallPlace('tabs', 'same.ts', 'whole', 1)?.scrollTop, 111);
  assert.equal(recallPlace('tabs', 'same.ts', 'whole', 2)?.scrollTop, 222);

  noteListKey('tabs', 'same.ts:9:9', 2, 'full');
  assert.equal(recallPlace('tabs', 'same.ts', 'whole', 1)?.scrollTop, 111, 'another tab is untouched');
  assert.equal(recallPlace('tabs', 'same.ts', 'whole', 2), null, 'the changed tab is forgotten');
}

// A terminal can change its working directory without changing session, tab or
// diff mode. Canonical repository roots therefore separate both list identity
// and the saved reading position.
{
  noteListKey('roots', 'same.ts:1:0', 0, 'full', '/repo/one');
  noteListKey('roots', 'same.ts:1:0', 0, 'full', '/repo/two');
  rememberPlace('roots', 'same.ts', 'whole', place(333), 0, '/repo/one');
  rememberPlace('roots', 'same.ts', 'whole', place(777), 0, '/repo/two');

  assert.equal(recallPlace('roots', 'same.ts', 'whole', 0, '/repo/one')?.scrollTop, 333);
  assert.equal(recallPlace('roots', 'same.ts', 'whole', 0, '/repo/two')?.scrollTop, 777);

  noteListKey('roots', 'same.ts:9:9', 0, 'full', '/repo/two');
  assert.equal(recallPlace('roots', 'same.ts', 'whole', 0, '/repo/one')?.scrollTop, 333);
  assert.equal(recallPlace('roots', 'same.ts', 'whole', 0, '/repo/two'), null);
}

console.log('diffViewState: ok');
