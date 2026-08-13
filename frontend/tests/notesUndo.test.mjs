import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * Undo in the note has to be the note's own.
 *
 * A textarea keeps its own undo history, but the browser empties it whenever
 * the value is assigned from code — which happens on every tab switch. Left to
 * the browser, Ctrl+Z in a note opened a moment ago does nothing at all.
 */
const notes = readFileSync(
  new URL('../src/lib/components/MainPanel/Notes.svelte', import.meta.url),
  'utf8',
);

assert.match(notes, /function undoNote/, 'the note keeps its own undo');
assert.match(notes, /function redoNote/, 'and its own redo');

// Ctrl+Y as well as Ctrl+Shift+Z: a textarea only offers the latter, and the
// user asked for both.
assert.match(
  notes,
  /event\.key === 'y' \|\| \(event\.key === 'z' && event\.shiftKey\)/,
  'redo should answer to Ctrl+Y and Ctrl+Shift+Z',
);

/**
 * Every path that replaces the text must clear the history with it.
 *
 * This is the failure that would matter: undo walking back into the previous
 * tab's note and writing it into this one. Three places assign to `notes` —
 * the successful load, the no-session case, and the load failure — and all
 * three have to reset.
 */
const assignments = notes.match(/^\s*notes = (?:content \|\| )?'';?$|^\s*notes = content \|\| '';$/gm) || [];
assert.ok(
  assignments.length >= 3,
  `expected the three assignments that replace the note, found ${assignments.length}`,
);

const resets = notes.match(/resetHistory\(\)/g) || [];
// One definition plus one call per assignment.
assert.ok(
  resets.length >= assignments.length + 1,
  `every path that replaces the text must reset the history: ${assignments.length} assignments, ${resets.length - 1} resets`,
);

/**
 * Dictation must not wipe the field's undo history either.
 *
 * Assigning to el.value is what cleared it, taking hand-typed text with it.
 * execCommand('insertText') is deprecated but is the only edit the browser
 * records as undoable, and it works in every engine this ships on.
 */
const dictation = readFileSync(
  new URL('../src/lib/utils/dictationField.ts', import.meta.url),
  'utf8',
);

assert.match(
  dictation,
  /execCommand\('insertText'/,
  'dictated text has to be inserted in a way the browser can undo',
);

const insert = dictation.match(/function insertAtCursor[\s\S]*?\n  }/);
assert.ok(insert, 'insertAtCursor is missing');
assert.match(
  insert[0],
  /if \(!inserted\)/,
  'the direct assignment must remain only as a fallback',
);

console.log('notesUndo: ok');
