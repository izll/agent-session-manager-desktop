import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

// Three records on one tab index, from an old store, made the command palette
// throw on opening (each_key_duplicate), and its stuck open flag silenced every
// shortcut after it. A list shows one tab per index.

test('one tab per index, the first of each, and the main index left out', async () => {
  const { firstPerIndex } = await import('../src/lib/utils/firstPerIndex.ts');
  const tabs = [{ index: 1, id: 'a' }, { index: 1, id: 'b' }, { index: 2, id: 'c' }, { index: 0, id: 'd' }, { index: 1, id: 'e' }];
  assert.deepEqual(firstPerIndex(tabs, (t) => t.index, 0).map((t) => t.id), ['a', 'c']);
  assert.deepEqual(firstPerIndex(tabs, (t) => t.index).map((t) => t.id), ['a', 'c', 'd']);
  assert.deepEqual(firstPerIndex(undefined, (t) => t.index), []);
});

test('the palette and the stopped tab bar list one tab per index', () => {
  assert.match(read('../src/lib/components/Dialogs/CommandPalette.svelte'),
    /for \(const tab of firstPerIndex\(session\.followedWindows/);
  assert.match(read('../src/lib/components/MainPanel/TabBar.svelte'),
    /const followedTabs = firstPerIndex\(sess\.followedWindows/);
  assert.match(read('../src/lib/components/Dialogs/SaveAsTemplateDialog.svelte'),
    /\{#each tabs as tab \(tab\.id \|\| tab\.index\)\}/);
});

test('the backend reports one status per tab index', () => {
  const app = read('../../app.go');
  assert.match(app, /listed := map\[int\]bool\{mainWindowIdx: true\}/);
  assert.match(app, /if !listed\[fw\.Index\] && !fw\.Stopped \{\n\t+listed\[fw\.Index\] = true/);
});
