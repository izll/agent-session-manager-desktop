import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The last of the session-versus-tab gaps.
 *
 * Each is the same mistake: reading the session where the tab is what matters.
 * A tab carries its own agent and can be opened in its own directory, so any
 * answer derived from the session alone is wrong on some tab somewhere.
 */
const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8');
const quickOpen = read('../src/lib/components/MainPanel/FileQuickOpen.svelte');
const browser = read('../src/lib/components/MainPanel/FileBrowser.svelte');
const tabBar = read('../src/lib/components/MainPanel/TabBar.svelte');
const app = read('../src/App.svelte');

// Quick-open searched the session's tree while the file tree beside it showed
// the tab's — one screen, two answers.
assert.match(quickOpen, /export let windowIdx = 0;/, 'quick-open must know which tab it is for');
assert.match(
  quickOpen,
  /App\.SearchSessionFileIndex\(sessionId, includeAll, windowIdx\)/,
  'the index search must be scoped to the tab',
);
assert.match(
  quickOpen,
  /App\.SearchSessionFileContents\(sessionId, q, false, includeAll, windowIdx\)/,
  'the content search must be scoped to the tab',
);
assert.match(browser, /windowIdx=\{\$selectedWindowIdx \?\? 0\}/, 'and the browser must pass it');

// The Resume button offered the action based on the session's agent, while the
// handler behind it correctly used the tab's — they could disagree.
const supportsResume = tabBar.match(/\$: agentSupportsResume = \(\(\) => \{[\s\S]*?\}\)\(\);/);
assert.ok(supportsResume, 'agentSupportsResume should still exist');
assert.match(
  supportsResume[0],
  /followedWindows\?\.find/,
  "the button must ask the tab's agent, as the handler already does",
);

// The picker accepts a path override and keys its cache on it; nothing ever
// supplied one, so a tab elsewhere was offered the session directory's history.
assert.match(app, /pathOverride=\{pendingResumePath\}/, 'the resume picker must be given the tab directory');

console.log('tabScopeRemaining: ok');
