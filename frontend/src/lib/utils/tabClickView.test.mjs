// Clicking a tab does two different things depending on which tab it is.
//
// Another tab: the view is left to that tab's own memory, so a tab left on its
// notes comes back to them. That was a deliberate fix — forcing the terminal on
// every click overwrote what had just been restored.
//
// The tab you are already on: the selection cannot change, so the click can
// only mean "show me this tab" — and that is the terminal, whatever view is
// sitting over it.
//
// Mirrors handleTabClick in TabBar.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

function clickTab(state, index) {
  if (index === state.selected) {
    return { ...state, view: 'terminal', diffOpen: false };
  }
  // Switching tabs restores what that tab was left on.
  return { ...state, selected: index, view: state.memory[index] ?? 'terminal' };
}

const STATE = {
  selected: 1,
  view: 'notes',
  diffOpen: false,
  memory: { 1: 'notes', 2: 'browser' },
};

test('clicking the current tab returns to the terminal', () => {
  assert.equal(clickTab(STATE, 1).view, 'terminal');
});

test('clicking the current tab also closes the diff', () => {
  const withDiff = { ...STATE, diffOpen: true };
  assert.equal(clickTab(withDiff, 1).diffOpen, false);
});

test('clicking another tab restores what that tab was on', () => {
  // Not the terminal: the whole point of the per-tab memory.
  assert.equal(clickTab(STATE, 2).view, 'browser');
});

test('a tab with nothing remembered opens on the terminal', () => {
  assert.equal(clickTab(STATE, 3).view, 'terminal');
});

test('clicking the current tab while already on the terminal changes nothing', () => {
  const onTerminal = { ...STATE, view: 'terminal' };
  assert.deepEqual(clickTab(onTerminal, 1), onTerminal);
});
