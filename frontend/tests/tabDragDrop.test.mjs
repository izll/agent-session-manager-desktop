import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const bar = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url), 'utf8');

// Reordering tabs was reported as hard to aim and as sometimes doing nothing
// at all when the tab was released. Both came from the same place.

// dragleave fires for every child of the tab as well — the status dot, the
// name, the close button — so clearing the marker on any of them made it
// flicker as the cursor crossed a tab, and left it cleared under the cursor at
// the moment of the drop. Measured in a browser: entering the dot fires
// dragleave on the tab, with the dot as the target.
test('leaving a tab is told apart from entering one of its children', () => {
  const leave = bar.slice(bar.indexOf('function handleTabDragLeave'));
  assert.match(leave.slice(0, 600), /relatedTarget/,
    'dragleave is acted on without checking where the cursor went');
  assert.match(leave.slice(0, 600), /\.contains\(/,
    'a child of the tab is not recognised as still being inside it');
});

// The marker was a left border whichever way the tab came from, so half of
// every drag pointed at the wrong gap: dropping after a tab drew the line
// before it.
test('the marker takes the side the cursor is nearest', () => {
  assert.match(bar, /dragOverAfter/,
    'nothing records which side of the tab the drop would land on');
  assert.match(bar, /box\.left \+ box\.width \/ 2/,
    'the side is not decided by which half of the tab the cursor is over');

  assert.match(bar, /class:tab-drop-before=/);
  assert.match(bar, /class:tab-drop-after=/);
});

// The backend removes the dragged tab and then inserts it, so a target to its
// right shifts down by one on the way. Without the adjustment the tab lands
// one place short of the line that was drawn for it.
test('the target position allows for the dragged tab being removed first', () => {
  const drop = bar.slice(bar.indexOf('async function handleTabDrop'));
  assert.match(drop.slice(0, 1200), /if \(fromPos < toPos\) toPos--/,
    'dropping to the right lands one place short of where the marker was');
});

// A drop that changes nothing must not go to the backend, but the old guard
// refused any drop on the dragged tab itself — including one on its far side,
// which is a real move.
test('a tab can be dropped on its own far side', () => {
  const drop = bar.slice(bar.indexOf('async function handleTabDrop'));
  assert.doesNotMatch(drop.slice(0, 400), /draggingTabIndex === arrayIdx/,
    'dropping on the dragged tab is refused outright, including its far side');
  assert.match(drop.slice(0, 1400), /if \(fromPos === toPos\) return/,
    'a drop that changes nothing still calls the backend');
});

// The arithmetic itself, against the reorder the backend performs.
//
// Pattern-matching the source says the adjustment is there; this says it is
// right. The backend removes the dragged tab and then inserts it at toPos
// (session/instance.go, ReorderTabs), which is what the second function here
// reproduces.
test('a tab lands in the gap the marker pointed at', () => {
  const toPosition = (fromPos, arrayIdx, after) => {
    let toPos = after ? arrayIdx + 1 : arrayIdx;
    if (fromPos < toPos) toPos--;
    return toPos;
  };
  const reorder = (order, fromPos, toPos) => {
    const out = [...order];
    const [item] = out.splice(fromPos, 1);
    out.splice(toPos, 0, item);
    return out;
  };

  const tabs = ['A', 'B', 'C', 'D'];
  const cases = [
    { from: 0, over: 1, after: true, want: 'BACD', what: 'A after B' },
    { from: 0, over: 3, after: true, want: 'BCDA', what: 'A to the end' },
    { from: 3, over: 0, after: false, want: 'DABC', what: 'D to the front' },
    { from: 3, over: 1, after: false, want: 'ADBC', what: 'D before B' },
    { from: 1, over: 2, after: true, want: 'ACBD', what: 'B after C' },
    { from: 2, over: 1, after: false, want: 'ACBD', what: 'C before B' },
    { from: 0, over: 0, after: true, want: 'ABCD', what: 'A onto its own side' },
  ];

  for (const { from, over, after, want, what } of cases) {
    const toPos = toPosition(from, over, after);
    const got = from === toPos ? tabs : reorder(tabs, from, toPos);
    assert.equal(got.join(''), want, `${what}: toPos=${toPos}`);
  }
});
