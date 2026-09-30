import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The commit history button sat after the branch list, so with many branches
// it was at the end of a long scroll. It is the menu's second item, right
// under the title.
test('the history button comes before the branch list', () => {
  const src = readFileSync(new URL('../src/lib/components/common/GitBranchBadge.svelte', import.meta.url), 'utf8')
    .replace(/\r\n/g, '\n');
  const menu = src.slice(src.indexOf('<div class="branch-menu"'));
  const title = menu.indexOf('class="branch-menu-title"');
  const history = menu.indexOf("new CustomEvent('git:show-history')");
  const list = menu.indexOf('class="branch-list"');
  assert.ok(title >= 0 && history > title && list > history,
    'the history button is not between the title and the branch list');
});

test('the history button is the size of the branch rows', () => {
  const src = readFileSync(new URL('../src/lib/components/common/GitBranchBadge.svelte', import.meta.url), 'utf8')
    .replace(/\r\n/g, '\n');
  const rule = src.slice(src.indexOf('  .branch-menu-action {'), src.indexOf('  .branch-menu-action:hover'));
  assert.match(rule, /font-size: 13px;/, 'the button inherits the page font and comes out larger than the rows');
});
