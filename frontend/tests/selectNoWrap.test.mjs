import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const select = readFileSync(
  new URL('../src/lib/components/common/Select.svelte', import.meta.url), 'utf8');

function rule(name) {
  // The declarations of one CSS rule, by its selector.
  const at = select.indexOf(name + ' {');
  if (at < 0) return '';
  return select.slice(at, select.indexOf('}', at));
}

// A name too long for the control used to wrap, making the field two rows tall
// and pushing the rest of the form down. Measured in a browser against the
// real component CSS: the trigger was 60px with the old rule and 41px with
// this one, in a 170px-wide field.
test('the chosen value stays on one line', () => {
  const value = rule('.select-value');
  assert.match(value, /white-space:\s*nowrap/,
    'a long option wraps, making the control two rows tall');
  assert.match(value, /text-overflow:\s*ellipsis/,
    'the cut text needs an ellipsis to show it continues');

  // Without this the ellipsis never appears: a flex item defaults to
  // min-width:auto, which refuses to shrink below its content, so the text
  // wraps however nowrap is set.
  assert.match(value, /min-width:\s*0/,
    'a flex item will not shrink below its content without min-width: 0');
});

// The list sizes itself to its widest option rather than to the control, so a
// name too long for a narrow field is readable in the one place there is room
// for it. Measured in a browser: a 196px control opened a 209px list, and the
// long option was no longer cut.
test('the list is as wide as it needs, not as wide as the control', () => {
  assert.match(select, /minWidth = `\$\{rect\.width\}px`/,
    'the control gives the list its minimum width, not its fixed width');
  assert.doesNotMatch(select, /dropdownRef\.style\.width = `\$\{rect\.width\}px`/,
    'forcing the list to the control width makes long options unreadable');
});

// Grown past the right edge, the list has to come back rather than hang off
// the window. Measured: a control at x=814 opened a list at x=799.
test('a wide list stays on screen', () => {
  assert.match(select, /overflowRight/,
    'nothing brings a list grown past the right edge back on screen');
  assert.match(select, /Math\.max\(VIEWPORT_MARGIN,/,
    'the list can be pushed off the left edge while avoiding the right');
});

// A wrapped row is taller than its neighbours, which makes the list hard to
// scan and throws off the height the dropdown was positioned against.
test('the options in the list stay on one line', () => {
  const option = rule(':global(.select-dropdown .select-option)');
  assert.match(option, /white-space:\s*nowrap/,
    'a long option wraps inside the dropdown');
  assert.match(option, /text-overflow:\s*ellipsis/);
});

// Cut text that cannot be read anywhere is worse than text that wraps. The
// full label has to stay reachable.
test('a truncated name can still be read', () => {
  assert.match(select, /class="select-value" title=\{displayText\}/,
    'the chosen value is cut with no way to see it in full');
  assert.match(select, /title=\{option\.label\}/,
    'an option in the list is cut with no way to see it in full');
});
