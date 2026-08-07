import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The quick-jump rows once carried a plain colour dot taken from the session's
 * `color`. It said nothing a name does not already say, and it was wrong twice
 * over: rendered only when a colour existed, so rows started at different
 * columns; and applied as a raw `background`, which silently ignores "auto"
 * and "gradient-…" — both common in a real list.
 *
 * What the marker shows now is activity, and the colours are applied the way
 * each kind is coloured where it lives.
 */
const source = readFileSync(
  new URL('../src/lib/components/Dialogs/QuickJumpDialog.svelte', import.meta.url),
  'utf8',
);

// Activity, not decoration.
assert.match(
  source,
  /class="activity \{row\.activity\}"/,
  'the row marker should show busy/waiting/idle, which is what a name cannot say',
);
assert.doesNotMatch(
  source,
  /\{#if row\.colour\}/,
  'a conditional marker lets rows start at different columns',
);

// Both status sources, since a tab and a session are tracked separately.
assert.match(source, /tabStatuses/, 'a tab entry takes its activity from the tab statuses');
assert.match(source, /\$activities\[/, 'a session entry takes its activity from the session activities');

// A stopped session has no live activity to report, and saying "idle" would
// claim it is running.
assert.match(
  source,
  /status !== 'running'\s*\n?\s*\?\s*'stopped'/,
  'a session that is not running must read as stopped rather than idle',
);

// Colours resolved through the shared helpers rather than used raw.
assert.match(source, /getNameStyle/, 'session rows should be coloured like the session list');
assert.match(source, /getContrastColor/, '"auto" tab text means readable-on-that-background');
assert.match(source, /getGradientCSS/, 'gradient names must be resolved to CSS');

// The sidebar already names these states; a second vocabulary would let the
// two disagree about the same thing.
assert.match(
  source,
  /sidebar\.status(Busy|Waiting|Idle|Stopped)/,
  'activity labels should reuse the sidebar wording',
);

// getNameStyle deliberately produces a `color`, and a gradient is not one — it
// skips them entirely. The sidebar paints gradients onto the text instead, and
// this list has to do the same or a gradient session renders plain.
assert.match(
  source,
  /background-clip: text/,
  'a gradient name must be painted onto the text, as the sidebar does',
);

// background-clip: text clips to its own box, so padding, overflow and an
// ellipsis on the same element are painted over as well. The sidebar keeps the
// two apart — layout outside, gradient on a bare span inside — and this has to
// match or the gradient does not look like the sidebar's.
const nameRule = source.match(/\n {2}\.name \{([\s\S]*?)\n {2}\}/);
assert.ok(nameRule, '.name rule is missing');
assert.doesNotMatch(
  nameRule[1],
  /padding/,
  'padding belongs on the inner span; on .name the gradient is clipped to it too',
);
assert.match(
  source,
  /<span style=\{row\.gradient\}>/,
  'the gradient must go on a bare span of its own, as the sidebar does',
);

// One rule, not two: a second .name block silently overrode the first.
assert.equal(
  (source.match(/\n {2}\.name \{/g) || []).length,
  1,
  'there should be exactly one .name rule',
);

// An entry is called what the user named it. The fallback matters as much as
// the name: entries stored before naming existed have no label, and must still
// read as the thing they point at rather than as a blank row.
assert.match(
  source,
  /name: entry\.label \|\|/,
  'the label should be used when set, with the target name as the fallback',
);

// Renaming starts from what the row shows — an edit, not a retype. An entry
// with no name of its own still displays one, so an empty field would make the
// user retype something already on screen.
assert.match(
  source,
  /editingText = target\.label \|\| \(resolved\[index\]\?\.defaultName/,
  'the rename field should open with the name shown on the row',
);

// A rename is easy to regret, and the suggestion is derived rather than
// remembered, so there has to be a way back to it.
assert.match(
  source,
  /function resetEditingName/,
  'there should be a way to restore the suggested name while editing',
);

// Storing a name identical to the suggestion would pin what the session and
// tab are called today, so the entry would keep the old name after a rename.
assert.match(
  source,
  /chosen === resolved\[index\]\?\.defaultName \? '' : chosen/,
  'a name matching the suggestion should be stored as no name at all',
);

// While a row is being renamed the field holds the name; showing the row's
// name as well put the old one right beside what was being typed.
assert.match(
  source,
  /\{#if editingIndex !== row\.index\}[\s\S]{0,200}class="name"/,
  'the row name should be hidden while that row is being edited',
);

console.log('quickJumpSwatch: ok');
