import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(
  new URL('../src/lib/components/Dialogs/GitHistoryDialog.svelte', import.meta.url), 'utf8');

const rule = (selector) => {
  const at = src.indexOf(`  ${selector} {`);
  assert.ok(at >= 0, `${selector} is gone`);
  return src.slice(at, src.indexOf('}', at));
};

// In a window that is not full width, "Changes only" wrapped onto two lines
// and the row stretched the arrow buttons beside it to the same height. The
// path gives way instead: it is already cut from the directory end.
test('the diff header buttons keep their size when the pane is narrow', () => {
  assert.match(rule('.selected-nav'), /flex-shrink: 0/, 'the buttons are squeezed');
  assert.match(rule('.selected-nav'), /align-items: center/,
    'a taller button stretches the others to its height');
  assert.match(rule('.nav-btn'), /white-space: nowrap/, 'a button label can wrap');
  assert.match(rule('.selected-stats'), /flex-shrink: 0/, 'the +/− counts are squeezed');
});

// A short sentence stretched across the whole column read as a bar with words
// stuck at its left end.
test('the unpushed summary is as wide as its text', () => {
  assert.match(rule('.unpushed-summary'), /width: fit-content/);
});
