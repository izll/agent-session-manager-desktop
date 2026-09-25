import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

// The server cloud's right arc reaches x=23, and its 2-unit stroke half a unit
// past the 24-unit box, so the box cut its edge off — visible on a tab. The
// icon is drawn in a box one unit larger on every side.
const CLOUD = '<path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/>';

for (const path of [
  '../src/App.svelte',
  '../src/lib/components/MainPanel/MainPanel.svelte',
  '../src/lib/components/MainPanel/TabBar.svelte',
]) {
  test(`${path}: the server cloud is not clipped`, () => {
    const src = read(path);
    const at = src.indexOf(CLOUD);
    assert.ok(at > 0, 'the cloud icon is gone; this test needs updating');
    const svg = src.slice(src.lastIndexOf('<svg', at), at);
    assert.match(svg, /viewBox="-1 -1 26 26"/, 'the cloud is drawn in a box that cuts its stroke');
  });
}
