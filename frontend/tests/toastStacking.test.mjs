import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const toastSrc = readFileSync(
  new URL('../src/lib/components/common/Toast.svelte', import.meta.url), 'utf8');
const slotsSrc = readFileSync(
  new URL('../src/lib/components/common/toastSlots.ts', import.meta.url), 'utf8');

// Several Toast components are mounted independently — session errors, tab
// errors, folder errors, dictation — and none knows about the others. Fixed at
// the same point, two showing together drew on top of each other and both
// messages became unreadable.
test('a toast is positioned by its slot, not at a fixed point', () => {
  assert.match(toastSrc, /style="top: \{slotOffset\}px/,
    'the toast no longer takes its position from its slot');
  assert.ok(!/\.toast\s*\{[^}]*\btop:\s*20px/.test(toastSrc),
    'the toast is pinned to a fixed top again, so two of them will overlap');
});

test('a slot is claimed while visible and released afterwards', () => {
  assert.match(toastSrc, /claimToastSlot\(\)/, 'no slot is claimed');
  assert.match(toastSrc, /releaseToastSlot\(/, 'a slot is never released');
  assert.match(toastSrc, /onDestroy\(\(\) => \{[\s\S]*?releaseToastSlot/,
    'a toast destroyed while visible keeps its slot forever');
});

// The allocation itself: two live toasts must never be handed the same seat,
// and a seat freed by one must be reusable by the next.
test('slots are handed out one at a time and reused', async () => {
  const module = await import('../src/lib/components/common/toastSlots.ts')
    .catch(() => null);
  if (!module) {
    // The .ts is not loadable without a transform; check the source instead.
    assert.match(slotsSrc, /while \(taken\.has\(slot\)\) slot\+\+/,
      'slots are not searched for the lowest free one');
    assert.match(slotsSrc, /taken\.add\(slot\)/, 'a claimed slot is not recorded');
    assert.match(slotsSrc, /taken\.delete\(slot\)/, 'a released slot is not freed');
    return;
  }
  const first = module.claimToastSlot();
  const second = module.claimToastSlot();
  assert.notEqual(first, second, 'two toasts were given the same slot');
  module.releaseToastSlot(first);
  assert.equal(module.claimToastSlot(), first, 'a freed slot was not reused');
});
