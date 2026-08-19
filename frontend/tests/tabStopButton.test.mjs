import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The start/stop button has to follow the pane, not only the app's own record.
 *
 * `stopped` on a followed window records a stop the app performed. A shell
 * closed with Ctrl+D never goes through that path, so the field stayed false
 * while the pane was dead — and the button went on offering to stop a tab that
 * had already exited.
 *
 * tmux knows the truth, and the window list already carries it as Dead; the tab
 * itself is drawn from that flag. Only this button was reading the other one.
 */
const tabBar = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url),
  'utf8',
);

const stoppedCheck = tabBar.match(/\$: currentTabStopped = \(\(\) => \{[\s\S]*?\}\)\(\);/);
assert.ok(stoppedCheck, 'currentTabStopped should still exist');

assert.match(
  stoppedCheck[0],
  /windows\.find\(w => w\.Index === winIdx\)/,
  'the button must consult the live window list',
);
assert.match(
  stoppedCheck[0],
  /live\?\.Dead/,
  "a dead pane means stopped, whoever killed it",
);
// The stored field stays as the fallback: a tab stopped through the app is
// respawned as a dead pane too, but the record is what survives a restart of
// the app itself.
assert.match(
  stoppedCheck[0],
  /fw\?\.stopped/,
  'the stored flag remains the fallback',
);

console.log('tabStopButton: ok');
