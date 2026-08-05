// Svelte scopes a component's CSS, but scoping does not protect its class
// NAMES: a global utility of the same name still applies to the element.
//
// This cost an afternoon. A row for a shortcut that cannot be rebound carried
// class="fixed", and Tailwind ships `.fixed { position: fixed }` — so those
// rows were lifted out of the document flow and painted over their neighbours.
// It looked like a layout bug in the component, and four plausible-looking CSS
// changes were made there before the computed style was actually measured
// (position: "fixed" on exactly the rows that used the class).
//
// The names below are Tailwind utilities that a component would plausibly reach
// for as a semantic class. Using one is not always wrong — but as a class the
// component also styles itself, it is.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

/** Utilities whose names read like state or role, so are easy to pick by
 *  accident, and whose effect is drastic enough to break a layout. */
const DANGEROUS = new Set([
  'fixed', 'absolute', 'relative', 'sticky', 'static',
  'block', 'inline', 'flex', 'grid', 'hidden', 'table',
  'visible', 'invisible', 'collapse',
]);

function svelteFiles(dir, out = []) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) svelteFiles(path, out);
    else if (entry.endsWith('.svelte')) out.push(path);
  }
  return out;
}

test('no component toggles a class that collides with a Tailwind utility', () => {
  const offenders = [];
  for (const file of svelteFiles('src')) {
    const source = readFileSync(file, 'utf8');
    // class:name={...} — the directive form, which is how a component says
    // "this element is in this state".
    for (const match of source.matchAll(/class:([a-zA-Z][\w-]*)\s*=/g)) {
      if (DANGEROUS.has(match[1])) {
        offenders.push(`${file}: class:${match[1]}`);
      }
    }
  }
  assert.deepEqual(offenders, [],
    'these class names are Tailwind utilities and will be applied globally, ' +
    'whatever the component\'s own scoped rules say');
});
