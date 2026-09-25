import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { transformSync } from 'esbuild';

// A Codex tab's YOLO badge follows what Codex reports, and says so when YOLO
// was asked for but is not in effect — its background server ignores the flag.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

const { code } = transformSync(read('../src/lib/utils/yoloBadge.ts'), { loader: 'ts', format: 'esm' });
const { yoloBadge } = await import(`data:text/javascript,${encodeURIComponent(code)}`);

test('the badge for each reading', () => {
  assert.equal(yoloBadge({ yolo: true }), 'on');
  assert.equal(yoloBadge({ yolo: true, yoloNotInEffect: true }), 'on');
  assert.equal(yoloBadge({ yolo: false, yoloNotInEffect: true }), 'notInEffect');
  assert.equal(yoloBadge({ yolo: false }), '');
  assert.equal(yoloBadge(undefined), '');
});

const item = read('../src/lib/components/Sidebar/SessionItem.svelte');

test('every tab row shows the warning badge, not only the plain one', () => {
  const rows = item.match(/\{#if yoloBadge\(tab\) && showYolo\}.*\{\/if\}/g) ?? [];
  assert.equal(rows.length, 3, 'busy, waiting and idle rows each draw the badge');
  for (const row of rows) {
    assert.match(row, /class:not-in-effect=\{yoloBadge\(tab\) === 'notInEffect'\}/);
    assert.match(row, /'sessionItem\.yoloNotInEffect'/);
  }
  assert.doesNotMatch(item, /\{#if tab\.yolo && showYolo\}/, 'a row still ignores the warning');
});

test('a single-tab session shows it next to its name', () => {
  assert.match(item, /singleTabYolo = tabStatuses\.length === 1 \? yoloBadge\(tabStatuses\[0\]\)/);
  assert.match(item, /class:not-in-effect=\{nameYolo === 'notInEffect'\}/);
  assert.match(item, /\.badge\.yolo\.not-in-effect \{[^}]*line-through/);
});

test('a tab row with only the warning is still drawn', () => {
  assert.match(item, /!!yoloBadge\(tab\) && showYolo/);
});

test('every locale explains the warning', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  for (const file of readdirSync(dir)) {
    const locale = JSON.parse(readFileSync(new URL(file, dir), 'utf8'));
    assert.ok(locale['sessionItem.yoloNotInEffect'], `${file} has no sessionItem.yoloNotInEffect`);
  }
});
