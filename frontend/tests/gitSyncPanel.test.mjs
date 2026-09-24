import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const { canOfferPush, canOfferPull, canRunSync, chosenRemote, outcomeKey } =
  await import('../src/lib/utils/gitSync.ts');

const syncGo = readFileSync(new URL('../../git_sync.go', import.meta.url), 'utf8');
const badge = readFileSync(
  new URL('../src/lib/components/common/GitBranchBadge.svelte', import.meta.url), 'utf8');
const panel = readFileSync(
  new URL('../src/lib/components/common/GitSyncPanel.svelte', import.meta.url), 'utf8');
const en = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/en.json', import.meta.url), 'utf8'));

const badgeState = (over = {}) => ({
  upstream: 'origin/main', behind: 0, unpushed: 0, unpushedKnown: true, onServer: false, ...over,
});

// The badge shows no ↑ when the count cannot be trusted; a push offered
// there would publish an unknown number of commits.
test('push is offered only for a known, non-zero count on this computer', () => {
  assert.equal(canOfferPush(badgeState({ unpushed: 2 })), true);
  assert.equal(canOfferPush(badgeState({ unpushed: 2, unpushedKnown: false })), false);
  assert.equal(canOfferPush(badgeState({ unpushed: 0 })), false);
  assert.equal(canOfferPush(badgeState({ unpushed: 2, onServer: true })), false,
    'a server tab would push this computer\'s checkout, not the one the tab works in');
  assert.equal(canOfferPush(null), false);
});

test('pull is offered only when behind an upstream, on this computer', () => {
  assert.equal(canOfferPull(badgeState({ behind: 1 })), true);
  assert.equal(canOfferPull(badgeState({ behind: 1, upstream: '' })), false);
  assert.equal(canOfferPull(badgeState({ behind: 0 })), false);
  assert.equal(canOfferPull(badgeState({ behind: 1, onServer: true })), false);
});

const pushPreview = (over = {}) => ({
  direction: 'push', setUpstream: false, remotes: ['origin'], remote: 'origin', diverged: false, total: 2, ...over,
});

test('a pull that cannot fast-forward cannot be started', () => {
  const pull = { ...pushPreview(), direction: 'pull' };
  assert.equal(canRunSync(pull, '', false), true);
  assert.equal(canRunSync({ ...pull, diverged: true }, '', false), false);
  assert.equal(canRunSync({ ...pull, total: 0 }, '', false), false);
});

// Several remotes and no hint is the user's choice; the button waits for it
// rather than guessing, and a name that is not a remote never counts.
test('a set-upstream push waits for a remote to be chosen', () => {
  const ambiguous = pushPreview({ setUpstream: true, remotes: ['alpha', 'beta'], remote: '' });
  assert.equal(canRunSync(ambiguous, '', false), false);
  assert.equal(canRunSync(ambiguous, 'gamma', false), false);
  assert.equal(canRunSync(ambiguous, 'beta', false), true);
  assert.equal(chosenRemote(ambiguous, 'beta'), 'beta');

  const suggested = pushPreview({ setUpstream: true, remotes: ['origin', 'fork'], remote: 'origin' });
  assert.equal(chosenRemote(suggested, ''), 'origin');
  assert.equal(chosenRemote(suggested, 'fork'), 'fork');
  assert.equal(canRunSync(suggested, '', true), false, 'a second click while one is running');
});

// Every outcome the backend can report is explained, in every locale the
// coverage test checks against English.
test('every backend outcome has its own explanation', () => {
  const block = syncGo.slice(syncGo.indexOf('gitSyncPushed '), syncGo.indexOf('gitSyncFailed ') + 40);
  const outcomes = [...block.matchAll(/=\s*"(\w+)"/g)].map((m) => m[1]);
  assert.ok(outcomes.length >= 10, `found only ${outcomes.length} outcomes`);
  for (const outcome of outcomes) {
    assert.equal(outcomeKey(outcome), `gitSync.outcome.${outcome}`,
      `${outcome} falls back to the generic failure`);
    assert.ok(en[`gitSync.outcome.${outcome}`], `${outcome} has no English text`);
  }
  assert.equal(outcomeKey('somethingNew'), 'gitSync.outcome.failed');
});

// The counts are separate buttons beside the branch button, not inside it:
// a button in a button is invalid, and the click would also open the menu.
test('the counts are their own controls, gated on the decisions above', () => {
  const main = badge.slice(badge.indexOf('class="git-branch-main"'));
  const mainEnd = main.indexOf('</button>');
  assert.ok(mainEnd > 0, 'the branch button is gone');
  assert.ok(!main.slice(0, mainEnd).includes('git-unpushed-badge'),
    'the ↑ pill is inside the branch button');
  assert.match(badge, /\{#if pushable\}/);
  assert.match(badge, /\$: pushable = canOfferPush\(/);
  assert.match(badge, /\$: pullable = canOfferPull\(/);
});

// Opening the panel lists; only the button pushes or pulls.
test('nothing is pushed or pulled without a click', () => {
  const load = panel.slice(panel.indexOf('async function load()'), panel.indexOf('async function run()'));
  assert.ok(load.length > 0, 'load() is gone');
  assert.ok(!/runGitPush|runGitPull/.test(load), 'opening the panel pushes or pulls');
  assert.match(panel, /on:click=\{run\}/, 'the action button no longer runs it');
  const mount = panel.slice(panel.indexOf('onMount(() =>'));
  assert.ok(!/\brun\b/.test(mount.slice(0, mount.indexOf('});'))), 'the panel runs on mount');
});
