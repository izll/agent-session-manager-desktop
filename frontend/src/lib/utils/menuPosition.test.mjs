import { test } from 'node:test';
import assert from 'node:assert';
import { readFileSync } from 'node:fs';
import { transformSync } from 'esbuild';

const src = readFileSync(new URL('./menuPosition.ts', import.meta.url), 'utf8');
const js = transformSync(src, { loader: 'ts', format: 'esm' }).code;
const { menuPosition } = await import(
  `data:text/javascript;base64,${Buffer.from(js).toString('base64')}`
);

const MARGIN = 8;

/** A stand-in for the menu element: fixed size, records what was written. */
function fakeNode(width, height) {
  return { offsetWidth: width, offsetHeight: height, style: {} };
}

function placeAt(anchor, { menu = [160, 200], window: win = [1000, 800] } = {}) {
  global.window = { innerWidth: win[0], innerHeight: win[1] };
  const node = fakeNode(menu[0], menu[1]);
  menuPosition(node, anchor);
  return { left: parseInt(node.style.left, 10), top: parseInt(node.style.top, 10) };
}

test('a menu with room below opens at the cursor', () => {
  assert.deepEqual(placeAt({ x: 100, y: 100 }), { left: 100, top: 100 });
});

test('a menu near the bottom flips above the cursor', () => {
  // 200-tall menu, 800-tall window, click at 700: 700+200 overflows, so the
  // menu goes above — its BOTTOM edge at the cursor.
  assert.deepEqual(placeAt({ x: 100, y: 700 }), { left: 100, top: 500 });
});

test('the last row of a long list stays fully on screen', () => {
  const { top } = placeAt({ x: 40, y: 795 }, { menu: [160, 240] });
  assert.ok(top >= MARGIN, `top ${top} must clear the margin`);
  assert.ok(top + 240 <= 800, `bottom ${top + 240} must stay in the window`);
});

test('a menu wider than the space to the right is clamped', () => {
  const { left } = placeAt({ x: 980, y: 100 });
  assert.equal(left, 1000 - 160 - MARGIN);
});

test('a menu taller than the window clamps rather than going off the top', () => {
  // No position fits; the top edge is the least bad one, and it must not end
  // up negative (which would hide the first entries).
  const { top } = placeAt({ x: 100, y: 400 }, { menu: [160, 900] });
  assert.ok(top >= MARGIN, `top ${top} must not be off-screen`);
});

test('a click at the very edge is pulled inside', () => {
  const { left, top } = placeAt({ x: 0, y: 0 });
  assert.equal(left, MARGIN);
  assert.equal(top, MARGIN >= 0 ? 0 : MARGIN); // y=0 has room below, so unchanged
});

test('update() repositions for a new anchor', () => {
  global.window = { innerWidth: 1000, innerHeight: 800 };
  const node = fakeNode(160, 200);
  const handle = menuPosition(node, { x: 10, y: 10 });
  handle.update({ x: 500, y: 700 });
  assert.equal(parseInt(node.style.left, 10), 500);
  assert.equal(parseInt(node.style.top, 10), 500); // flipped above
});
