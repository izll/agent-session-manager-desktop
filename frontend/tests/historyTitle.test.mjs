import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The history dialog's panes fold away leftwards, and the header takes over
 * saying what they showed: fold the branches and it names the branch, fold the
 * commit list too and it adds the commit's subject.
 *
 * Without that, folding both leaves a dialog full of one file's diff with
 * nothing on screen saying which commit — or which branch — it belongs to.
 */
const source = readFileSync(
  new URL('../src/lib/components/Dialogs/GitHistoryDialog.svelte', import.meta.url),
  'utf8',
);

const title = source.match(/\$: headerTitle = \[([\s\S]*?)\]\.filter\(Boolean\)\.join\((.*?)\);/);
assert.ok(title, 'headerTitle is gone; the header no longer reports the folded panes');

const [, parts, separator] = title;

// The parts, in the order they appear in the header.
assert.match(parts, /history\.title/, 'the dialog name comes first');
assert.match(
  parts,
  /branchesCollapsed \?[^\n]*branch/,
  'the branch is added only when the branch pane is folded',
);
assert.match(
  parts,
  /commitsCollapsed \?[^\n]*selectedCommit/,
  "the commit's subject is added only when the commit list is folded",
);

// A plain hyphen, not a dash: it is what was asked for and it matches the
// naming used elsewhere in the app.
assert.equal(separator.trim(), "' - '", `separator is ${separator}, want ' - '`);

// filter(Boolean) is what makes "commits folded, branches not" produce
// "Commit history - subject" rather than an empty gap between two separators.
assert.match(
  title[0],
  /\.filter\(Boolean\)/,
  'empty parts must drop out, or folding only the commit list leaves a gap',
);

// One element that truncates as a whole. Built as two — a title plus a span —
// each would ellipsis separately, and a commit subject is long enough to need
// it.
assert.match(
  source,
  /<h2 class="history-title" title=\{headerTitle\}>\{headerTitle\}<\/h2>/,
  'the header should be one string, with the full text available on hover',
);

const rule = source.match(/\n {2}\.history-title \{([\s\S]*?)\n {2}\}/);
assert.ok(rule, '.history-title rule is missing');
assert.match(rule[1], /text-overflow: ellipsis/, 'a long subject must truncate');
assert.match(rule[1], /white-space: nowrap/, 'the title must stay on one line');
assert.match(
  rule[1],
  /min-width: 0/,
  'a flex item will not shrink below its content without this, so it would push the buttons out',
);

// The commit message panel sits above the file tree and the diff, so its
// height decides theirs. Sized as a share of the dialog, every commit gave the
// panes below a different amount of room — selecting one resized the tree and
// the diff under the cursor, which reads as the layout jumping about.
const detail = source.match(/\n {2}\.commit-detail \{([\s\S]*?)\n {2}\}/);
assert.ok(detail, '.commit-detail rule is missing');
assert.doesNotMatch(
  detail[1],
  /max-height: \d+%/,
  'a percentage height makes every commit resize the panes below it',
);
assert.match(
  detail[1],
  /height: \d+px/,
  'a fixed height keeps the panes below still; a long message scrolls instead',
);
assert.match(
  detail[1],
  /overflow-y: auto/,
  'a message longer than the fixed height has to scroll rather than be cut off',
);

console.log('historyTitle: ok');
