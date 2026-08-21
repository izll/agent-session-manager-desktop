import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * A fork has to leave you looking at the branch.
 *
 * Both modes create something and then have to show it. The new-tab case
 * already did; the new-session case created the session, closed the dialog, and
 * left the view exactly where it was — so from the user's side nothing had
 * happened at all. The branch was running, in the sidebar, under a name only
 * the clock knew.
 *
 * The 'forked' event is not a substitute: nothing listens for it. It is
 * dispatched for whoever might one day want it, which means the switching has
 * to happen here.
 */
const source = readFileSync(
  new URL('../src/lib/components/Dialogs/ForkDialog.svelte', import.meta.url),
  'utf8',
);

const submit = source.match(/async function handleSubmit\(\)([\s\S]*?)\n {2}\}/);
assert.ok(submit, 'handleSubmit is missing');

const [tabBranch, sessionBranch] = (() => {
  const at = submit[0].indexOf('} else {');
  assert.notEqual(at, -1, 'the two fork modes should be an if/else');
  return [submit[0].slice(0, at), submit[0].slice(at)];
})();

// New tab: switch to the tab that was made.
assert.match(tabBranch, /selectWindow\(newIdx\)/, 'a forked tab must be selected');

// New session: switch to the session that was made, and to its first tab —
// selectSession alone can leave the window index pointing at a tab number the
// new session does not have.
assert.match(
  sessionBranch,
  /selectSession\(newSession\.id\)/,
  'a forked session must be selected, or the fork looks like it did nothing',
);
assert.match(
  sessionBranch,
  /selectWindow\(0\)/,
  'and its first tab, since the previous session may have been on a higher one',
);

// Selected BEFORE the dialog closes, so a close that resets state cannot undo
// it.
const selectAt = sessionBranch.indexOf('selectSession(');
const closeAt = sessionBranch.indexOf('close()');
assert.ok(selectAt !== -1 && closeAt !== -1 && selectAt < closeAt,
  'the branch should be selected before the dialog closes');

// And the list has to be refreshed first, or the session being selected is not
// in it yet.
const loadAt = sessionBranch.indexOf('loadSessions()');
assert.ok(loadAt !== -1 && loadAt < selectAt,
  'the session list must be reloaded before selecting the new session');

// Nothing listens for this event, so it cannot be what performs the switch.
const listeners = readFileSync(
  new URL('../src/lib/components/MainPanel/MainPanel.svelte', import.meta.url),
  'utf8',
).includes('on:forked');
assert.equal(listeners, false,
  'if something starts listening for forked, revisit whether the switch belongs there instead');

/**
 * A forked session can be put in a different group.
 *
 * It used to inherit the original's silently, which is a reasonable default and
 * a poor rule: a branch is often an experiment, and experiments belong
 * somewhere other than the work they came from.
 *
 * Only for a forked SESSION — a forked tab stays inside the session it came
 * from, so there is nothing to choose.
 */
assert.match(
  source,
  /\{#if forkMode === 'session' && \$groups\.length > 0\}/,
  'the group picker belongs to the new-session mode only, and only where groups exist',
);
assert.match(source, /bind:value=\{selectedGroupId\}/, 'the picker needs to be bound');

// Defaulted to the original's group, so the common case is unchanged.
assert.match(
  source,
  /selectedGroupId = get\(sessions\)\.find\(s => s\.id === get\(selectedSessionId\)\)\?\.groupId \|\| ''/,
  'the picker should start on the group the fork would have inherited',
);

// Applied only when it differs — and compared against the inherited value
// rather than tested for emptiness, because "no group" is a real choice that a
// truthiness test would silently ignore.
assert.match(
  sessionBranch,
  /if \(submitted\.groupId !== submitted\.inheritedGroup\)/,
  'moving to no group at all must be honoured, not treated as "unset"',
);

console.log('forkSelectsBranch group: ok');
