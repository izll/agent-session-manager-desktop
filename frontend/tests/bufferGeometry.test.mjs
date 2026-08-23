import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The dictation buffer window remembers where it was put.
 *
 * Stored as pixels rather than as a fraction of the window: it is a panel the
 * user drags to a spot that suits them, and a proportion of a different-sized
 * screen is not that spot. The cost is a saved rectangle that no longer fits —
 * a laptop undocked from a second monitor, an app window that was full-screen
 * and now is not — and since the panel is dragged by its own header, one that
 * opens off-screen cannot be brought back at all.
 *
 * So the clamping is the part worth guarding.
 */
const source = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url),
  'utf8',
);

function body(name) {
  const marker = 'function ' + name + '(';
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, name + ' is missing');
  const end = source.indexOf('\n  }', start);
  return source.slice(start, end);
}

// A remembered rectangle is fitted to the window it is opening in, never used
// raw.
const fit = body('fitToViewport');
assert.match(fit, /window\.innerWidth/, 'the fit must be against the current window width');
assert.match(fit, /window\.innerHeight/, 'the fit must be against the current window height');
assert.match(fit, /MIN_BUFFER_W/, 'a floor is needed, or a saved sliver is unusable');
assert.match(
  fit,
  /Math\.max\(0, window\.innerWidth - w\)/,
  'the panel must be kept far enough in that its header stays reachable',
);

assert.match(
  body('applyStoredBufferGeometry'),
  /fitToViewport/,
  'stored geometry must be fitted before it is applied',
);

// The window can shrink after the panel is placed.
assert.match(
  source,
  /addEventListener\('resize', keepBufferOnScreen\)/,
  'a shrinking window must pull the panel back into view',
);
assert.match(
  source,
  /removeEventListener\('resize', keepBufferOnScreen\)/,
  'the resize listener must be removed with the component',
);

// Following a shrunk window must not overwrite the size chosen in a large one,
// or maximising again would leave the panel at whatever fitted while small.
assert.doesNotMatch(
  body('keepBufferOnScreen'),
  /rememberBufferGeometry|saveSettings/,
  'clamping to a smaller window must not be saved as the user preference',
);

// Saving happens when a gesture ends, not while it is in flight.
assert.match(
  body('onDragEnd'),
  /projectId === \$activeProjectId\) rememberBufferGeometry\(projectId\)/,
  'the position should be saved when the drag ends',
);
assert.doesNotMatch(
  body('onDragMove'),
  /rememberBufferGeometry|saveSettings/,
  'saving during a drag would write the settings file dozens of times a second',
);

console.log('bufferGeometry: ok');
