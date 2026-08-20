import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * Failures in the sessions store reach the screen.
 *
 * The store had 26 writers and no reader: every error.set() went into a value
 * nothing displayed. Anything failing outside a component carrying its own
 * toast — deleting from the sidebar, renaming, reordering, switching project —
 * failed in silence, and the user's only clue was that nothing happened.
 */
const app = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8');
const store = readFileSync(
  new URL('../src/lib/stores/sessions.ts', import.meta.url),
  'utf8',
);

// Still worth reporting: if the writers ever disappear, this test should be
// revisited rather than silently passing on an empty store.
const writers = store.match(/error\.set\(/g) ?? [];
assert.ok(writers.length > 5, 'the store should still be recording failures');

assert.match(
  app,
  /import \{ error as sessionError \} from '\.\/lib\/stores\/sessions'/,
  'the app must subscribe to the store that records session failures',
);
assert.match(
  app,
  /\$: if \(\$sessionError\)/,
  'a reader is what turns a recorded failure into a visible one',
);
// Cleared after showing, or the same message can never appear twice.
assert.match(app, /sessionError\.set\(null\)/, 'the error must be cleared once shown');
assert.match(
  app,
  /<Toast bind:show=\{showSessionError\}/,
  'and it must be shown in a toast',
);

console.log('sessionErrorSurface: ok');
