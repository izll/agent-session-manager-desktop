import { test } from 'node:test';
import assert from 'node:assert';
import { readFileSync } from 'node:fs';
import { transformSync } from 'esbuild';

const src = readFileSync(new URL('./openMenu.ts', import.meta.url), 'utf8');
const js = transformSync(src, { loader: 'ts', format: 'esm' }).code;
const mod = await import(
  `data:text/javascript;base64,${Buffer.from(js).toString('base64')}`
);
const { claimMenu, releaseMenu } = mod;

/** A menu that records whether it was told to close. */
function menu() {
  const m = { closed: false };
  m.close = () => { m.closed = true; releaseMenu(m.close); };
  return m;
}

test('opening a second menu closes the first', () => {
  const a = menu(), b = menu();
  claimMenu(a.close);
  claimMenu(b.close);
  assert.equal(a.closed, true, 'the first menu should have been closed');
  assert.equal(b.closed, false, 'the menu just opened must stay open');
});

test('re-claiming from the same menu does not close it', () => {
  // A component can re-open its own menu (right-clicking a second row in the
  // same list); closing itself would leave nothing on screen.
  const a = menu();
  claimMenu(a.close);
  claimMenu(a.close);
  assert.equal(a.closed, false);
});

test('a third menu closes only the second', () => {
  const a = menu(), b = menu(), c = menu();
  claimMenu(a.close);
  claimMenu(b.close);
  a.closed = false; // ignore the first eviction
  claimMenu(c.close);
  assert.equal(b.closed, true);
  assert.equal(a.closed, false, 'an already-evicted menu must not be closed again');
  assert.equal(c.closed, false);
});

test('a late release from an evicted menu does not clear the current one', () => {
  // The evicted menu's own close path may run after the new menu registered.
  // If release cleared the slot unconditionally, the next open would leave
  // TWO menus on screen — the bug this module exists to prevent.
  const a = menu(), b = menu(), c = menu();
  claimMenu(a.close);
  claimMenu(b.close);
  releaseMenu(a.close);   // stale release, arriving after b claimed
  claimMenu(c.close);
  assert.equal(b.closed, true, 'b still held the slot and must be closed');
});

test('after a release the slot is free', () => {
  const a = menu(), b = menu();
  claimMenu(a.close);
  releaseMenu(a.close);
  a.closed = false;
  claimMenu(b.close);
  assert.equal(a.closed, false, 'a released menu must not be closed again');
});
