import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';

const dir = new URL('../src/lib/components/Dialogs/', import.meta.url);
const files = readdirSync(dir).filter(f => f.endsWith('.svelte'));

test('there are dialogs to check', () => {
  assert.ok(files.length > 20, `only found ${files.length} dialogs`);
});

// A click that lands next to a dialog throws away whatever was typed into it,
// with no confirmation and no undo. Losing a half-filled new-tab form to a
// stray click is not worth the convenience of closing without aiming.
for (const file of files) {
  test(`${file} does not close on a backdrop click`, () => {
    const src = readFileSync(new URL(file, dir), 'utf8');
    const at = src.indexOf('class="dialog-overlay"');
    if (at < 0) return; // not overlay-based

    // The overlay element runs from its tag's start to the closing '>'.
    const tagStart = src.lastIndexOf('<', at);
    const tag = src.slice(tagStart, src.indexOf('>', at) + 1);

    assert.ok(!/on:click/.test(tag),
      `the overlay still has a click handler, so clicking beside the dialog ` +
      `discards it:\n${tag}`);
  });
}

// Removing the backdrop click removes a way out, so the other one has to work:
// every dialog must still be dismissable from the keyboard.
for (const file of files) {
  test(`${file} can still be closed with Escape`, () => {
    const src = readFileSync(new URL(file, dir), 'utf8');
    if (!src.includes('class="dialog-overlay"')) return;
    assert.match(src, /['"]Escape['"]/,
      'no Escape handling, and the backdrop click is gone — the dialog traps the user');
  });
}
