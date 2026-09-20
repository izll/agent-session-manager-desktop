import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(
  new URL('../src/lib/components/Dialogs/ServerManagerDialog.svelte', import.meta.url), 'utf8');

// Eleven operations in this dialog share operationGeneration, and each bumps
// it. A listing guarded by that counter is discarded whenever any of the
// others starts — a connection test, a save, a host-key accept — and what was
// on screen before stays there, looking like the new data never arrived.
//
// A listing only needs protecting from a NEWER listing.
test('the session listing is guarded by its own generation', () => {
  const start = src.indexOf('async function loadServerSessions(');
  assert.ok(start > 0, 'loadServerSessions is gone; this test needs rewriting');
  const body = src.slice(start, src.indexOf('\n  }', start));

  assert.match(body, /\+\+sessionsGeneration/,
    'the listing takes the shared counter, so any other operation discards it');
  assert.ok(!/operationGeneration/.test(body),
    'the listing still compares against the shared counter');
  assert.equal((body.match(/sessionsGeneration/g) || []).length, 4,
    'every early return in the listing must check its own generation');
});

// The guard has to exist at all: without one, a slow reply for a server the
// user has already navigated away from would overwrite the current list.
test('the listing still discards a reply it no longer wants', () => {
  const start = src.indexOf('async function loadServerSessions(');
  const body = src.slice(start, src.indexOf('\n  }', start));

  assert.match(body, /generation !== sessionsGeneration/,
    'a stale listing reply is no longer discarded');
  assert.match(body, /!show \|\| generation !== sessionsGeneration/,
    'a reply arriving after the dialog closed is no longer discarded');
});
