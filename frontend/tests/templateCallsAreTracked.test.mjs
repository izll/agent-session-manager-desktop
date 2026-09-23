import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compile } from 'svelte/compiler';

// A function called with no arguments in a template is compiled untracked:
// Svelte 5's legacy mode keeps Svelte 4's rule that a template depends only on
// what it names. Whatever state the function reads inside does not update the
// template. The colour dialogs' previews were such calls, so picking a
// background left the preview unchanged — only "full row", which reads the
// colour directly in the template, showed it.
//
// Pass what the function depends on as arguments instead; then the template
// names it and tracks it.
const src = fileURLToPath(new URL('../src', import.meta.url));

function components(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap(entry =>
    entry.isDirectory() ? components(join(dir, entry.name))
      : entry.name.endsWith('.svelte') ? [join(dir, entry.name)] : []);
}

test('no template calls a function without naming what it depends on', () => {
  const offenders = [];
  for (const file of components(src)) {
    const { js } = compile(readFileSync(file, 'utf8'), { filename: file, generate: 'client' });
    const bare = js.code.match(/\$\.untrack\([A-Za-z_$][\w$]*\)/g);
    if (bare) offenders.push(`${file.slice(src.length + 1)}: ${[...new Set(bare)].join(', ')}`);
  }
  assert.deepEqual(offenders, [],
    'these template calls read state the template does not track, so they never update');
});
