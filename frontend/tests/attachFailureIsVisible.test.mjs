import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const termSrc = readFileSync(
  new URL('../src/lib/components/MainPanel/Terminal.svelte', import.meta.url), 'utf8');

// An attach that failed used to leave no trace on screen.
//
// `error` was assigned in six places and read in none: the pane fell back to
// one of the idle jokes, so a tab that could not be opened at all looked the
// same as one with nothing running. The only record of the reason was the log.
//
// The case that surfaced it: a tab restored from the trash kept its server but
// was given a local window index, so the attach asked that server about a
// window it had never created. Clicking the tab did nothing, said nothing, and
// offered nothing to act on.

test('the reason an attach failed is shown, not only logged', () => {
  const template = termSrc.slice(termSrc.indexOf('</script>'));
  assert.match(template, /\{error\}/,
    'error is assigned but never rendered, so a failed attach shows an idle ' +
    'joke and the reason reaches only the log');
  assert.match(template, /terminal\.attachFailed/,
    'the failure has no heading of its own');
});

// The joke is for an idle pane. A pane that failed must not draw one.
test('a failed attach outranks the idle placeholder', () => {
  const at = termSrc.indexOf('$: attachFailed =');
  assert.ok(at > 0, 'nothing distinguishes a failed attach from an idle pane');
  const line = termSrc.slice(at, termSrc.indexOf('\n', at));

  assert.match(line, /!isAttached/, 'a pane that did attach would claim failure');
  assert.match(line, /error !== ''/, 'an idle pane with no error would claim failure');
  assert.match(line, /!parkedTab/,
    'a parked tab is waiting to be started, not failed, and has its own text');

  // The icon and the text both have to follow it, or the pane says one thing
  // and shows another.
  const icon = termSrc.slice(termSrc.indexOf('$: placeholderIcon ='));
  assert.match(icon.slice(0, 300), /attachFailed/,
    'the icon still comes from the idle rotation on a failed attach');
});

// A stale reason behind a working pane is its own bug: the next successful
// attach has to clear it.
test('the error is cleared when an attach succeeds', () => {
  const at = termSrc.indexOf('isAttached = true;');
  assert.ok(at > 0);
  assert.match(termSrc.slice(at, at + 300), /error = '';/,
    'the last failure stays on screen after the pane starts working');
});

// The heading is user-facing text, so it exists in every language the app
// ships — a missing key renders as the key itself.
test('the heading is translated everywhere', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const locales = readdirSync(dir).filter(name => name.endsWith('.json'));
  assert.ok(locales.length >= 20, `only ${locales.length} locales found`);

  for (const name of locales) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    const text = strings['terminal.attachFailed'];
    assert.ok(typeof text === 'string' && text.trim() !== '',
      `${name} has no terminal.attachFailed, so the pane shows the key itself`);
  }
});

// After this computer restarts a session, a tab on a server comes back without
// a window there until it is started. The attach fails — there is nothing to
// attach to — but that is a tab waiting to be started, and showing it as an
// error ("WebSocket connection failed") sent the user looking for a fault.
test('a tab missing on its server reads as parked, not as failed', () => {
  assert.match(termSrc, /\$: remoteMissing =[\s\S]{0,200}\.missing/,
    'the pane does not read whether its server holds the tab\'s window');

  const failed = termSrc.slice(termSrc.indexOf('$: attachFailed ='));
  assert.match(failed.slice(0, failed.indexOf('\n')), /!remoteMissing/,
    'a tab waiting on its server is still shown as an attach error');

  const key = termSrc.slice(termSrc.indexOf('$: placeholderKey ='));
  assert.match(key.slice(0, 300), /remoteMissing\s*\?\s*'terminal\.tabParked'|parkedTab \|\| remoteMissing\s*\?\s*'terminal\.tabParked'/,
    'a tab waiting on its server does not say it is waiting to be started');
});

// The pane read unreachable and missing from tabStatuses, which carries only
// agent tabs and only for sessions with more than one — so a terminal tab, or
// a session's single agent tab, never showed either mark. It reads the
// availability map, which covers every tab.
test('the pane reads availability for every tab, not only multi-agent ones', () => {
  assert.match(termSrc, /\$tabAvailability\[targetSessionId\]/,
    'the pane does not read the per-tab availability');
  assert.doesNotMatch(termSrc, /\$tabStatuses\[targetSessionId\][^;]*\.(unreachable|missing)/,
    'unreachable/missing still come from tabStatuses, which drops terminal tabs');
});
