// Matching a key press against a binding compares every modifier in BOTH
// directions: a binding without Shift must not fire when Shift is held.
//
// This is what keeps Ctrl+P and Ctrl+Shift+P apart. They are two different
// shortcuts here — the saved-command picker and the command palette — so a
// one-directional check ("the binding wants Ctrl, and Ctrl is down") would fire
// both from a single press, and the user would get two dialogs.
//
// Mirrors eventMatches() in stores/shortcuts.ts.
import { test } from 'node:test';
import assert from 'node:assert/strict';

function eventMatches(e, binding) {
  if (e.key.toLowerCase() !== binding.key) return false;
  // Cmd counts as Ctrl, so a Mac user needs no separate binding.
  if (!!binding.ctrl !== (e.ctrlKey || e.metaKey)) return false;
  if (!!binding.shift !== e.shiftKey) return false;
  if (!!binding.alt !== e.altKey) return false;
  return true;
}

/** A KeyboardEvent as far as matching is concerned. */
function press(key, { ctrl = false, shift = false, alt = false, meta = false } = {}) {
  return { key, ctrlKey: ctrl, shiftKey: shift, altKey: alt, metaKey: meta };
}

test('a binding matches its own combination', () => {
  const binding = { key: 'n', ctrl: true, shift: true };
  assert.ok(eventMatches(press('N', { ctrl: true, shift: true }), binding));
});

test('a shifted letter arrives uppercase and still matches', () => {
  // e.key is 'N' for Ctrl+Shift+N. Comparing it raw would never match a
  // binding stored as 'n'.
  const binding = { key: 'n', ctrl: true, shift: true };
  assert.ok(eventMatches(press('N', { ctrl: true, shift: true }), binding));
  assert.ok(eventMatches(press('n', { ctrl: true, shift: true }), binding));
});

test('Ctrl+Shift+P does not also fire Ctrl+P', () => {
  const picker = { key: 'p', ctrl: true };              // saved commands
  const palette = { key: 'p', ctrl: true, shift: true }; // command palette

  const withShift = press('P', { ctrl: true, shift: true });
  assert.equal(eventMatches(withShift, picker), false,
    'Ctrl+Shift+P fired the Ctrl+P shortcut — one press, two actions');
  assert.ok(eventMatches(withShift, palette));

  const withoutShift = press('p', { ctrl: true });
  assert.ok(eventMatches(withoutShift, picker));
  assert.equal(eventMatches(withoutShift, palette), false);
});

test('an unwanted modifier prevents a match', () => {
  const binding = { key: 'k', ctrl: true };
  assert.equal(eventMatches(press('k', { ctrl: true, alt: true }), binding), false,
    'Alt was held but the binding does not ask for it');
});

test('Cmd stands in for Ctrl', () => {
  const binding = { key: 'k', ctrl: true };
  assert.ok(eventMatches(press('k', { meta: true }), binding),
    'a Mac user pressing Cmd+K got nothing');
});

test('a binding with no modifiers rejects a modified press', () => {
  const binding = { key: 'escape' };
  assert.ok(eventMatches(press('Escape'), binding));
  assert.equal(eventMatches(press('Escape', { ctrl: true }), binding), false);
});

// Two shortcuts on the same keys is a state the user cannot get out of except
// by finding the other one themselves, so the editor warns before accepting.
function sameBinding(a, b) {
  return a.key === b.key &&
    !!a.ctrl === !!b.ctrl &&
    !!a.shift === !!b.shift &&
    !!a.alt === !!b.alt;
}

test('conflicts are detected regardless of how the flags are written', () => {
  // One stored from a key event (explicit false), one from a default (omitted).
  const fromEvent = { key: 'f', ctrl: true, shift: true, alt: false };
  const fromDefault = { key: 'f', ctrl: true, shift: true };
  assert.ok(sameBinding(fromEvent, fromDefault),
    'absent and false must mean the same thing, or a real clash goes unreported');
});

test('differing only in a modifier is not a conflict', () => {
  assert.equal(
    sameBinding({ key: 'f', ctrl: true, shift: true }, { key: 'f', ctrl: true }),
    false);
});

// The global handler leaves immediately on a press with no modifier, before it
// touches the DOM — that early exit is what keeps typing cheap, so a shortcut
// bound to a bare key would never fire and would look broken.
function acceptableAsShortcut(binding) {
  return !!binding.ctrl || !!binding.alt;
}

test('a shortcut must carry Ctrl or Alt', () => {
  assert.ok(acceptableAsShortcut({ key: 'n', ctrl: true }));
  assert.ok(acceptableAsShortcut({ key: 'arrowup', alt: true }));
  assert.equal(acceptableAsShortcut({ key: 'n' }), false);
  assert.equal(acceptableAsShortcut({ key: 'n', shift: true }), false,
    'Shift alone still types a character, so it cannot carry a shortcut');
});

// "Switched off" and "never touched" are different states, and storing both as
// an empty entry would collapse them: turning a shortcut off would look exactly
// like restoring its default, so it could never be turned off at all.
//
// Mirrors effectiveBindings in stores/shortcuts.ts.
function resolve(defaults, override) {
  return override === undefined ? defaults : override;
}

test('an untouched shortcut follows its default', () => {
  const defaults = [{ key: 'n', ctrl: true, shift: true }];
  assert.deepEqual(resolve(defaults, undefined), defaults);
});

test('a stored empty list means off, not default', () => {
  const defaults = [{ key: 'n', ctrl: true, shift: true }];
  assert.deepEqual(resolve(defaults, []), [],
    'an off shortcut fell back to its default, so it could never be switched off');
});

test('restoring the default removes the entry rather than emptying it', () => {
  // What restoreDefaultBinding does: delete, not assign [].
  const overrides = { 'session.new': [{ key: 'q', ctrl: true }] };
  delete overrides['session.new'];
  const defaults = [{ key: 'n', ctrl: true, shift: true }];
  assert.deepEqual(resolve(defaults, overrides['session.new']), defaults);
});

test('a later change of default reaches anyone who never customised', () => {
  // The reason only customised shortcuts are stored: a saved copy of the
  // default would pin every existing user to the old keys forever.
  const oldDefault = [{ key: 'n', ctrl: true, shift: true }];
  const newDefault = [{ key: 'm', ctrl: true, shift: true }];
  assert.deepEqual(resolve(newDefault, undefined), newDefault);
  // Someone who DID customise keeps their own choice.
  const theirs = [{ key: 'q', ctrl: true }];
  assert.deepEqual(resolve(newDefault, theirs), theirs);
});
