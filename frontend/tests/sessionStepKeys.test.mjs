import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const keys = await import('../src/lib/utils/sessionStepKeys.ts');
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

const key = (k, mods = {}) => ({
  key: k, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, ...mods,
});

// A stand-in for shortcutForEvent with the given bindings in force.
const resolver = (bindings) => (e) => {
  for (const [id, match] of Object.entries(bindings)) if (match(e)) return id;
  return null;
};
const altArrow = (dir) => (e) => e.altKey && e.key === dir;

// The terminal refused Alt+Up/Down whatever the bindings were, so a pane never
// received them once the shortcut was rebound or switched off.
test('the default bindings are refused', () => {
  keys.registerShortcutResolver(resolver({
    'session.prev': altArrow('ArrowUp'), 'session.next': altArrow('ArrowDown'),
  }));
  assert.ok(keys.isSessionStepKey(key('ArrowUp', { altKey: true })));
  assert.ok(keys.isSessionStepKey(key('ArrowDown', { altKey: true })));
  assert.ok(!keys.isSessionStepKey(key('ArrowUp')), 'a plain arrow is the pane\'s');
});

test('switched off, Alt+Up/Down reach the pane', () => {
  keys.registerShortcutResolver(resolver({}));
  assert.ok(!keys.isSessionStepKey(key('ArrowUp', { altKey: true })));
  assert.ok(!keys.isSessionStepKey(key('ArrowDown', { altKey: true })));
});

test('rebound, the new keys are refused and the old ones are not', () => {
  keys.registerShortcutResolver(resolver({
    'session.prev': (e) => e.ctrlKey && e.key === 'k',
    'session.next': (e) => e.ctrlKey && e.key === 'j',
  }));
  assert.ok(!keys.isSessionStepKey(key('ArrowUp', { altKey: true })));
  assert.ok(keys.isSessionStepKey(key('k', { ctrlKey: true })),
    'the rebound key reaches the pane as well as stepping');
});

// The same key bound elsewhere too: the app acts on whichever it resolves to.
test('a key the app resolves to another shortcut is not refused as a step', () => {
  keys.registerShortcutResolver(resolver({ 'search.global': altArrow('ArrowUp') }));
  assert.ok(!keys.isSessionStepKey(key('ArrowUp', { altKey: true })));
});

// The app's handler returns early for a key without Ctrl, Cmd or Alt.
test('a key the app ignores is not refused', () => {
  keys.registerShortcutResolver(() => 'session.next');
  assert.ok(!keys.isSessionStepKey(key('ArrowDown')));
  keys.registerShortcutResolver(null);
  assert.ok(!keys.isSessionStepKey(key('ArrowDown', { altKey: true })),
    'with nothing registered, keys were refused that nothing would act on');
});

// The store cannot be imported by utils/terminal (settings imports it), so it
// is handed over by the component that makes every terminal.
test('the shortcut store is handed to the terminal, not imported by it', () => {
  const terminal = read('../src/lib/utils/terminal.ts');
  assert.doesNotMatch(terminal, /from '\.\.\/stores\/shortcuts'/,
    'utils/terminal imports the shortcut store: settings -> terminal -> shortcuts -> settings');
  assert.doesNotMatch(terminal, /event\.altKey && \(event\.key === 'ArrowUp'/,
    'Alt+Up/Down is still refused whatever the bindings are');
  const pane = read('../src/lib/components/MainPanel/Terminal.svelte');
  const register = pane.indexOf('registerShortcutResolver(shortcutForEvent)');
  assert.ok(register > 0, 'nothing tells the terminal which keys step');
  assert.ok(register < pane.indexOf('new TerminalPool('), 'registered after terminals exist');
});
