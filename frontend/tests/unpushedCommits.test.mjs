import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const store = read('../src/lib/stores/gitBranch.ts');
const badge = read('../src/lib/components/common/GitBranchBadge.svelte');
const history = read('../src/lib/components/Dialogs/GitHistoryDialog.svelte');
const panel = read('../src/lib/components/MainPanel/MainPanel.svelte');

// The count the header shows is the backend's "on no remote branch", not
// "ahead of upstream": a branch never pushed has no upstream, so the old ↑N
// showed nothing on exactly the branch whose every commit is only local.
test('the header badge counts unpushed commits, not ahead-of-upstream', () => {
  const fn = store.slice(store.indexOf('export function unpushedCount'));
  const body = fn.slice(0, fn.indexOf('\n}\n'));
  assert.match(body, /\.unpushed\b/, 'the badge does not read the unpushed count');
  assert.doesNotMatch(body, /upstream/,
    'the badge still depends on an upstream, so a never-pushed branch shows nothing');
  // With no remote every commit is "unpushed"; flagging them all is noise.
  assert.match(body, /hasRemote/, 'a repository with no remote would flag every commit');

  assert.match(badge, /\{#if unpushed > 0\}[\s\S]{0,200}git-unpushed-badge/,
    'the header shows no badge for unpushed commits');
});

// Without this the count stays at whatever it was before `git push` in a
// terminal until the user switches tabs.
test('the badge refreshes on its own while the window has focus', () => {
  const at = panel.indexOf('function startPathPolling');
  const body = panel.slice(at, panel.indexOf('\n  }\n', at));
  assert.match(body, /revalidateGitBranch\(\)/,
    'nothing re-reads the branch between tab switches, so a push goes unnoticed');
});

test('the history marks each unpushed commit and says how many', () => {
  assert.match(history, /class:unpushed=\{commit\.unpushed\}/,
    'unpushed commits look like every other commit in the list');
  assert.match(history, /\{#if commit\.unpushed\}[\s\S]{0,200}unpushed-tag/,
    'an unpushed commit carries no label saying so');
  assert.match(history, /unpushedTotal = page\.unpushed/,
    'the list does not take the branch-wide count from the page');
  // The summary sits outside the half-opacity title, which a child cannot undo.
  const title = history.slice(history.indexOf('<div class="pane-title">'));
  const titleBlock = title.slice(0, title.indexOf('</div>'));
  assert.doesNotMatch(titleBlock, /unpushed-summary/,
    'the summary is inside the dimmed title and would be drawn at half opacity');
});

test('every new string is translated', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  for (const name of readdirSync(dir).filter((n) => n.endsWith('.json'))) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of ['gitBranch.unpushed', 'history.unpushedShort', 'history.unpushedTitle']) {
      assert.ok(strings[key]?.trim(), `${name} has no ${key}`);
    }
    assert.match(strings['gitBranch.unpushed'], /\{count\}/,
      `${name}: gitBranch.unpushed lost its {count}, so the number is not shown`);
  }
});
