import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const dialogDir = new URL('../src/lib/components/Dialogs/', import.meta.url).pathname;

// A spinner drawn in the accent colour disappears on a button whose background
// IS the accent colour — the primary button is an accent gradient, so the
// update button's "working" spinner was all but invisible against it.
//
// currentColor follows whatever the button sets for its text, so a spinner
// inside one stays visible even if the button is restyled later.
test('a spinner inside a button takes the button colour, not the accent', () => {
  const offenders = [];

  for (const file of readdirSync(dialogDir).filter(n => n.endsWith('.svelte'))) {
    const src = readFileSync(dialogDir + file, 'utf8');

    // Only where a DIV spinner sits inside a button. An <svg class="spinner">
    // is drawn with stroke="currentColor" and already follows the button, so
    // it was never at risk — the fault is specific to a bordered div whose
    // colour is set in CSS.
    const divInButton = /class="btn[^"]*"[\s\S]{0,400}?<div class="spinner/.test(src);
    if (!divInButton) continue;

    // The rule that rescues it: a spinner under .btn using currentColor.
    const hasOverride = /\.btn\s+\.spinner\s*\{[^}]*currentColor/.test(src);
    if (!hasOverride) offenders.push(file);
  }

  assert.deepEqual(offenders, [],
    'these draw a spinner inside a button without taking the button colour, ' +
    'so it blends into an accent-coloured button');
});
