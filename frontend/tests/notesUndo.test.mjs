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
 * tab's note and writing it into this one. A failed load no longer fabricates
 * an empty note: it restores the target's cached draft through showDraft.
 */
assert.match(notes, /function showDraft[\s\S]*?notes = draft\.text/);
assert.match(notes, /notes = content \|\| '';[\s\S]*?resetHistory\(\)/,
  'a successful backend load resets history');
assert.match(notes, /showDraft\(\{ \.\.\.draft, loadError: String\(e\) \}\);[\s\S]*?resetHistory\(\)/,
  'a failed load restores only the target draft and resets history');
assert.doesNotMatch(notes, /Failed to load notes:[\s\S]{0,200}?notes = ''/,
  'a failed load must not become an empty successful document');

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
