import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The diff sits with the views, and knows which tab it is for.
 *
 * It used to live in the tab bar. That put it a level above the thing it
 * describes: a tab is a place the session runs, the diff is a way of looking at
 * one — and since a tab can be opened in its own directory, one session can
 * hold several tabs with a different diff each. Beside Files it answers the
 * same question about the same directory.
 */
const panel = readFileSync(
  new URL('../src/lib/components/MainPanel/MainPanel.svelte', import.meta.url),
  'utf8',
);
const tabBar = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url),
  'utf8',
);

// One home, not two: leaving it in both places is how they drift apart.
assert.doesNotMatch(tabBar, /class="tab diff-tab"/, 'the tab bar must not carry a diff tab any more');
assert.match(panel, /class="view-tab \{fullDiffActive \? 'active' : ''\}"/, 'the view bar must carry it');

// Availability follows the TAB's directory. The session-wide flag answers for
// the session path, and one session can hold tabs inside a repository and
// outside one — it offered the diff where there is none and withheld it where
// there is.
assert.match(panel, /App\.TabIsGitRepo\(sessionId, windowIdx\)/, "the button must ask about the tab's directory");
assert.doesNotMatch(
  panel.slice(panel.indexOf('class="view-tab {fullDiffActive')),
  /currentSession\?\.isGitRepo/,
  'the diff button must not be gated on the session-wide flag',
);

// Greyed out, not hidden: a control that vanishes reads as a bug, while a
// disabled one with a reason says what the tab is.
const button = panel.match(/class="view-tab \{fullDiffActive[\s\S]{0,600}?<\/button>/);
assert.ok(button, 'the diff button should be findable');
assert.match(button[0], /disabled=\{!tabIsGitRepo\}/, 'it must be disabled outside a repository');
assert.match(button[0], /notARepository/, 'and say why');

// A slow answer for a tab already left must not decide for the tab now on
// screen — the same generation guard the directory lookup uses.
assert.match(
  panel,
  /if \(generation !== liveTabPathGeneration\) return;\s*\n\s*tabIsGitRepo = answer;/,
  'the git check must be guarded against a stale reply',
);

// And an open diff closes when the tab it belongs to has no repository,
// otherwise it hangs there with its own button greyed out and no way back.
assert.match(
  panel,
  /\$: if \(fullDiffActive && !tabIsGitRepo\) \{\s*\n\s*closeFullDiff\(\);/,
  'an open diff must close on a tab without a repository',
);

console.log('diffViewTab: ok');
