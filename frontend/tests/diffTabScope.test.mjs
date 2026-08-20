import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The diff belongs to the tab, and reverting writes files.
 *
 * A tab can be opened in a directory of its own, so the session's path is the
 * wrong repository for it. Showing the wrong diff is confusing; reverting from
 * it is worse — the write landed in the session's repository, on a file the
 * user never saw. The two travel together deliberately: a diff that reloads on
 * a tab switch is what makes "revert what you are looking at" true.
 */
const diff = readFileSync(
  new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url),
  'utf8',
);

// Every call into the backend says which tab it is for.
const backendCalls = diff.match(/App\.(GetSessionDiff|GetFullDiff|GetSessionDiffFileList|GetFullDiffFileList|GetSessionDiffForFile|GetFullDiffForFile|RevertDiffFile|RevertDiffHunk)\(/g) ?? [];
assert.ok(backendCalls.length >= 8, `expected the diff entry points, found ${backendCalls.length}`);

// Matched to the end of the statement rather than to the first ')': the
// arguments contain calls of their own, and a lazy match stops inside them.
const withoutTab = (diff.match(/App\.(?:GetSessionDiff|GetFullDiff|RevertDiffFile|RevertDiffHunk)\([^;\n]*/g) ?? [])
  .filter((call) => !call.includes('tabIdx()') && !call.includes('target.windowIdx'));
assert.deepEqual(withoutTab, [], 'every diff call must pass the tab index');

assert.match(
  diff,
  /type DiffTarget = \{ sessionId: string; windowIdx: number; mode:/,
  'a destructive action must snapshot the target it was offered for',
);
assert.match(
  diff,
  /if \(!isCurrentTarget\(action\.target\)\)/,
  'a revert must be refused after its session, tab, or mode changes',
);

// The reload has to notice a tab change, or the screen keeps the previous
// repository's diff while the revert targets the new one.
assert.match(
  diff,
  /\$: diffKey = `\$\{\$selectedSessionId \?\? ''\}:\$\{\$selectedWindowIdx \?\? 0\}`/,
  'the diff must be keyed on session AND tab',
);
assert.match(diff, /if \(active && diffKey !== lastSessionId\)/, 'the guard must compare the tab key');

console.log('diffTabScope: ok');
