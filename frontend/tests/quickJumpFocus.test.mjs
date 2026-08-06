import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The quick-jump list holds focus itself and moves a cursor through its rows —
 * the rows are not focusable. That makes returning focus to the list something
 * the code has to do deliberately.
 *
 * Editing a note put focus in an input. When the input went away, on Escape or
 * on save, focus landed on the document body: the arrows stopped moving the
 * cursor and the number keys stopped jumping, which reads as the window having
 * quietly stopped working.
 */
const source = readFileSync(
  new URL('../src/lib/components/Dialogs/QuickJumpDialog.svelte', import.meta.url),
  'utf8',
);

function body(name) {
  const marker = 'function ' + name + '(';
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, name + ' is gone');
  const end = source.indexOf('\n  }', start);
  return source.slice(start, end);
}

// Both ways out of an edit have to hand focus back.
assert.match(body('commitEditing'), /focusList\(\)/, 'saving a note must return focus to the list');
assert.match(body('cancelEditing'), /focusList\(\)/, 'Escape must return focus to the list');

// Reloading after an edit must not send the cursor back to the top: the user
// is looking at the row they just edited.
assert.match(
  source,
  /keepCursor/,
  'a reload triggered by an edit should keep the cursor where it was',
);

// Tab is the other way people walk a list. The rows cannot receive focus, so
// left alone Tab leaves the dialog on the first press.
assert.match(
  source,
  /case 'Tab':/,
  'Tab should move the cursor rather than leaving the list',
);

// The edit button must not shift the row's contents as the cursor arrives, so
// it is hidden by visibility rather than removed from the layout.
const buttonRule = source.match(/\n {2}\.note-btn \{([\s\S]*?)\n {2}\}/);
assert.ok(buttonRule, '.note-btn rule is missing');
assert.match(
  buttonRule[1],
  /visibility: hidden/,
  'the button should keep its place in the layout, or names shift under the cursor',
);
assert.doesNotMatch(
  buttonRule[1],
  /display: none/,
  'display:none removes it from the layout and moves everything after it',
);

console.log('quickJumpFocus: ok');
