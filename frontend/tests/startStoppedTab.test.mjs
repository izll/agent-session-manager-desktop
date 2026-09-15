import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const appSrc = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8');
const storeSrc = readFileSync(
  new URL('../src/lib/stores/sessions.ts', import.meta.url), 'utf8');

// RestartTab respawns a pane, which needs a multiplexer session to respawn it
// into. A stopped session has none, so the backend answered "instance not
// running" and the dialog's own offer failed every time it was taken.
test('starting one tab of a stopped session does not go through restartTab', () => {
  const at = appSrc.indexOf('async function handleStartTab');
  assert.ok(at > 0, 'handleStartTab is gone');
  const fn = appSrc.slice(at, appSrc.indexOf('\n  }\n', at));

  assert.match(fn, /startTabOnly\(/,
    'the offer still restarts a pane, which a stopped session does not have');
  assert.match(fn, /status === 'stopped'/,
    'nothing distinguishes a stopped session from a running one');
});

// A running session still has to take the cheap path: restarting the one dead
// pane, not restarting the whole session around it.
test('a running session still restarts just the tab', () => {
  const at = appSrc.indexOf('async function handleStartTab');
  const fn = appSrc.slice(at, appSrc.indexOf('\n  }\n', at));
  assert.match(fn, /await restartTab\(target\.sessionId, target\.windowIdx\)/,
    'the running-session path lost its restart, so starting a tab rebuilds the session');
});

test('the store call is wired to the backend and refreshes the pane', () => {
  const at = storeSrc.indexOf('export async function startTabOnly');
  assert.ok(at > 0, 'startTabOnly is gone from the store');
  const fn = storeSrc.slice(at, storeSrc.indexOf('\n}\n', at));

  assert.match(fn, /App\.StartTabOnly\(id, windowIdx, target\.projectId\)/,
    'the call does not pin the project, so a project switch mid-flight retargets it');
  assert.match(fn, /dropPoolForWindow\(id, windowIdx\)/,
    'the cached terminal for this window survives, so the new pane never shows');
  assert.match(fn, /await loadSessions\(\)/, 'the session list is not refreshed');
});

test('the import is present, or the call is a runtime ReferenceError', () => {
  assert.match(appSrc, /import \{[^}]*startTabOnly[^}]*\} from '\.\/lib\/stores\/sessions'/s,
    'startTabOnly is called but never imported');
});
