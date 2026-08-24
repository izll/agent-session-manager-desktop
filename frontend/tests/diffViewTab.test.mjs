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

// The view bar survives the diff being open.
//
// It used to be replaced by it, so nothing said where you had come from and the
// only way back was the Diff control that had gone with the bar.
const diffBranch = panel.match(/\{#if fullDiffActive\}[\s\S]{0,700}?\{:else\}/);
assert.ok(diffBranch, 'the full diff should still be conditional');
assert.doesNotMatch(
  diffBranch[0],
  /class="view-tabs"/,
  'the view bar must not live inside the diff branch — it has to stay on screen',
);
assert.match(diffBranch[0], /<Diff active=\{visible\} initialMode="full" \/>/, 'the diff fills the area below it');

// And the view underneath stays marked, so pressing Diff again visibly returns
// somewhere rather than anywhere.
assert.match(
  panel,
  /class:behind-diff=\{fullDiffActive && activeView === 'terminal'\}/,
  'the covered view must remain marked while the diff is open',
);
assert.match(panel, /\.view-tab\.behind-diff \{/, 'and be styled apart from the active one');

// Choosing a view closes the diff covering it.
//
// The diff fills the area below the bar, so a view chosen while it is open was
// updating underneath it: the button highlighted and nothing moved. Every
// caller of selectView is asking to SEE a view, so the closing belongs there
// rather than being remembered at each call site — two of them were already
// compensating for it by hand.
const selectView = panel.match(/function selectView\(view: ViewName\) \{[\s\S]*?\n  \}/);
assert.ok(selectView, 'selectView should still exist');
assert.match(
  selectView[0],
  /fullDiffActive = false;/,
  'selecting a view must close the diff on top of it',
);

// closeFullDiff must NOT do the reverse: hiding the diff reveals the view the
// tab was already on, which is what makes "press Diff again to go back" true.
const closeFullDiff = panel.match(/function closeFullDiff\(\) \{[\s\S]*?\n  \}/);
assert.ok(closeFullDiff, 'closeFullDiff should still exist');
assert.doesNotMatch(
  closeFullDiff[0],
  /activeView = /,
  'closing the diff must leave the underlying view alone',
);

// A tab left showing its diff comes back to it.
//
// The diff is that tab's work: comparing two agents means going back and forth
// between their diffs, and demanding a press of Diff on every arrival makes the
// app fight the comparison. Notes, tasks and files deliberately do NOT come
// back — those are places you go to look at something, and restoring them read
// as the app losing track of where you were.
//
// This was removed once, when the diff covered the whole view bar and returning
// to one left no sign of where you were or how to leave. It sits under the bar
// now, with the tab and the view both still marked.
assert.match(panel, /const tabDiffMemory = new Set<string>\(\);/, 'the tabs left on the diff must be remembered');
assert.match(
  panel,
  /fullDiffActive = tabDiffMemory\.has\(key\);/,
  'returning to such a tab must restore its diff',
);
assert.match(
  panel,
  /if \(fullDiffActive\) tabDiffMemory\.add\(lastViewKey\);\s*\n\s*else tabDiffMemory\.delete\(lastViewKey\);/,
  'and leaving a tab must record whether the diff was open',
);
// The other views stay unremembered on purpose.
assert.match(panel, /activeView = 'terminal';/, 'arrival still lands on the terminal for every other view');

// The tab stays marked while its diff is open: the diff is a view OF that tab.
assert.match(
  tabBar,
  /class:active=\{\$selectedWindowIdx === win\.Index\}/,
  'a tab showing its diff must still read as the active tab',
);

console.log('diffViewTab: ok');
