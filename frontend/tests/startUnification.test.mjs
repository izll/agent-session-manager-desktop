import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const app = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8');

function bodyOf(name) {
  const at = app.indexOf(`function ${name}(`);
  assert.ok(at > 0, `${name} is gone`);
  const rest = app.slice(at);
  const end = rest.indexOf('\n  }\n');
  return rest.slice(0, end + 4);
}

// The three ways a session could start each decided for themselves how much to
// start, and only one of them asked. So the same button on the same session
// started one tab or all of them depending on which agent it ran and whether it
// had been resumed before.
test('the tab question comes before any resume decision', () => {
  const body = bodyOf('handleStart');
  const askedAt = body.indexOf('showStartDialog = true');
  const resumeAt = body.indexOf('resumeSessionId');
  assert.ok(askedAt > 0, 'handleStart no longer asks which tabs to start');
  assert.ok(resumeAt === -1 || askedAt < resumeAt,
    'a saved resume id still short-circuits the question, so that session ' +
    'starts everything without asking');
});

test('a session with tabs always asks', () => {
  const body = bodyOf('handleStart');
  assert.match(body, /followedWindows\?\.length \?\? 0\) > 0/,
    'the question is no longer gated on the session actually having tabs');
});

test('a session with no tabs is not asked and just starts', () => {
  const body = bodyOf('handleStart');
  assert.match(body, /startWholeSession\(\$selectedSession\)/,
    'a tabless session should start without a dialog');
});

// "Start all" from the dialog has to mean the same as starting a session that
// has no tabs — otherwise resuming works in one case and not the other.
test('start-all goes through the same path as a plain start', () => {
  const body = bodyOf('handleStartSession');
  assert.match(body, /startWholeSession\(/,
    'the dialog\'s "start all" bypasses the resume handling, so a session ' +
    'resumed from the button would start fresh from the dialog');
});

test('resuming still happens, just later', () => {
  const body = bodyOf('startWholeSession');
  assert.match(body, /resumeSessionId/, 'the saved resume id is no longer used');
  assert.match(body, /supportsResume/, 'the resume dialog is no longer offered');
});
