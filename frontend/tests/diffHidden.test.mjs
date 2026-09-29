import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// Files hidden from the diff view — by the view only. The matching rules run
// here as they are; the wiring into the diff is read from its source.

const {
  normaliseRule,
  ruleKind,
  ruleMatches,
  rulesHiding,
  isHidden,
  splitHidden,
  standaloneRules,
  folderOf,
  extensionPattern,
  hiddenByCount,
} = await import('../src/lib/utils/diffHidden.ts');

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const diffSrc = read('../src/lib/components/MainPanel/Diff.svelte');
const storeSrc = read('../src/lib/stores/diffHidden.ts');

// ── What a rule matches ─────────────────────────────────────────────────────

test('a file rule hides that path and nothing else', () => {
  assert.equal(ruleKind('src/app.ts'), 'file');
  assert.equal(ruleMatches('src/app.ts', 'src/app.ts'), true);
  assert.equal(ruleMatches('src/app.ts', 'src/app.tsx'), false);
  assert.equal(ruleMatches('src/app.ts', 'other/src/app.ts'), false);
  assert.equal(ruleMatches('app.ts', 'src/app.ts'), false);
});

test('a folder rule hides everything under that folder, at that place only', () => {
  assert.equal(ruleKind('build/'), 'folder');
  assert.equal(ruleMatches('build/', 'build/a.js'), true);
  assert.equal(ruleMatches('build/', 'build/deep/b.js'), true);
  assert.equal(ruleMatches('build/', 'buildx/a.js'), false);
  assert.equal(ruleMatches('build/', 'src/build/a.js'), false);
  assert.equal(ruleMatches('build/', 'build'), false);
});

test('a pattern without a slash matches the file name at any depth', () => {
  assert.equal(ruleKind('*.lock'), 'pattern');
  assert.equal(ruleMatches('*.lock', 'yarn.lock'), true);
  assert.equal(ruleMatches('*.lock', 'deep/in/Cargo.lock'), true);
  assert.equal(ruleMatches('*.lock', 'lock.txt'), false);
  assert.equal(ruleMatches('?.md', 'docs/a.md'), true);
  assert.equal(ruleMatches('?.md', 'docs/ab.md'), false);
});

test('a pattern with a slash is matched against the whole path', () => {
  assert.equal(ruleMatches('src/*.gen.ts', 'src/a.gen.ts'), true);
  assert.equal(ruleMatches('src/*.gen.ts', 'src/sub/a.gen.ts'), false, '* stays in one segment');
  assert.equal(ruleMatches('src/**/*.gen.ts', 'src/sub/deep/a.gen.ts'), true);
  assert.equal(ruleMatches('src/**/*.gen.ts', 'src/a.gen.ts'), true, '**/ also matches no folder');
  assert.equal(ruleMatches('gen/**', 'gen/x/y.txt'), true);
  assert.equal(ruleMatches('gen/**', 'other/gen/x.txt'), false);
});

test('a folder pattern matches folders by name, or by path when it has a slash', () => {
  assert.equal(ruleMatches('node_*/', 'web/node_modules/a/index.js'), true);
  assert.equal(ruleMatches('node_*/', 'node_modules.txt'), false);
  assert.equal(ruleMatches('web/node_*/', 'web/node_modules/x.js'), true);
  assert.equal(ruleMatches('web/node_*/', 'app/web/node_modules/x.js'), false);
});

test('regular-expression characters in a rule are taken literally', () => {
  assert.equal(ruleMatches('a+b(1).*', 'a+b(1).txt'), true);
  assert.equal(ruleMatches('a+b(1).*', 'aab(1)xtxt'), false);
});

test('rules are normalised the way the backend stores them', () => {
  assert.equal(normaliseRule('  ./src\\gen/ '), 'src/gen/');
  assert.equal(normaliseRule('/build//out/'), 'build/out/');
  assert.equal(normaliseRule('././a.txt'), 'a.txt');
  assert.equal(normaliseRule('   '), '');
});

// ── What the tabs show ──────────────────────────────────────────────────────

const files = [
  { path: 'src/app.ts', status: 'modified' },
  { path: 'build/out.js', status: 'added' },
  { path: 'build/map.js', status: 'added' },
  { path: 'yarn.lock', status: 'modified' },
  { path: 'README.md', status: 'added' },
];

test('hidden files leave the list the other tabs work from', () => {
  const { shown, hidden } = splitHidden(files, ['build/', 'yarn.lock']);
  assert.deepEqual(shown.map((f) => f.path), ['src/app.ts', 'README.md']);
  assert.deepEqual(hidden.map((f) => f.path), ['build/out.js', 'build/map.js', 'yarn.lock']);
  // No rules: nothing moves, and the caller gets a list of its own.
  const none = splitHidden(files, []);
  assert.deepEqual(none.shown, files);
  assert.notEqual(none.shown, files);
  assert.deepEqual(none.hidden, []);
});

test('showing a file again removes every rule that hides it', () => {
  const rules = ['build/', '*.js', 'README.md'];
  assert.deepEqual(rulesHiding(rules, 'build/out.js'), ['build/', '*.js']);
  assert.deepEqual(rulesHiding(rules, 'src/app.ts'), []);
  assert.equal(isHidden(rules, 'README.md'), true);
  assert.equal(isHidden(rules, 'src/app.ts'), false);
});

test('the Skipped tab lists a file rule once, as its file', () => {
  const rules = ['yarn.lock', 'build/', 'gone.txt', '*.snap'];
  const { hidden } = splitHidden(files, rules);
  // yarn.lock is listed as a hidden file; the folder, the pattern and the rule
  // whose file has no changes right now each get a row of their own.
  assert.deepEqual(standaloneRules(rules, hidden), ['build/', 'gone.txt', '*.snap']);
  assert.equal(hiddenByCount('build/', hidden), 2);
  assert.equal(hiddenByCount('*.snap', hidden), 0);
});

test('the menu offers the file\'s folder and its type', () => {
  assert.equal(folderOf('src/lib/a.ts'), 'src/lib/');
  assert.equal(folderOf('a.ts'), '');
  assert.equal(extensionPattern('src/yarn.lock'), '*.lock');
  assert.equal(extensionPattern('Makefile'), null);
  assert.equal(extensionPattern('.gitignore'), null, 'a dot file has no extension');
  assert.equal(extensionPattern('src/.env.local'), '*.local');
});

// ── Wiring ──────────────────────────────────────────────────────────────────

test('every tab but Skipped works from the files on show', () => {
  assert.match(diffSrc, /all: shownFiles\.length/);
  assert.match(diffSrc, /skipped: skippedFiles\.length/);
  assert.match(diffSrc, /statusFilter === 'skipped'\s*\?\s*skippedFiles/);
  assert.match(diffSrc, /: shownFiles\.filter\(f => \(f\.status \|\| 'modified'\) === statusFilter\)/);
  // Counted from the list before the status filter, never the raw diff.
  assert.doesNotMatch(diffSrc, /all: files\.length/);
});

test('Skipped comes after the status tabs', () => {
  assert.match(diffSrc, /\['all', \.\.\.GROUP_ORDER, 'skipped'\]/);
});

test('stepping walks the list on screen, so hidden files are passed over', () => {
  // stepOrder is visibleFiles (or the tree built from it); visibleFiles holds
  // no hidden file outside the Skipped tab.
  assert.match(diffSrc, /\$: stepOrder = treeView[\s\S]*?: visibleFiles;/);
  assert.match(diffSrc, /buildTreeRows<session\.DiffFileSummary>\(visibleFiles, collapsedDirs\)/);
});

test('the header counts only the files on show', () => {
  assert.match(diffSrc, /\$: fileCount = shownFiles\.length;/);
  assert.match(diffSrc, /\+\{shownTotals\.added\}/);
  assert.match(diffSrc, /-\{shownTotals\.removed\}/);
});

test('a fresh selection never lands on a hidden file', () => {
  const sync = diffSrc.slice(diffSrc.indexOf('function syncSelection()'));
  const body = sync.slice(0, sync.indexOf('\n  }\n'));
  assert.match(body, /splitHidden\(files, hiddenRulesOf\(hiddenRepo, get\(diffHiddenRules\)\)\)\.shown/);
  assert.match(body, /: shown\[0\]\.path/);
});

test('the rules are fetched with the file list, not after it', () => {
  const load = diffSrc.slice(diffSrc.indexOf('async function loadDiff()'));
  const repoAt = load.indexOf('hiddenRepoFor(sessionId, windowIdx, root)');
  const listAt = load.indexOf('App.GetSessionDiffFileList(');
  assert.ok(repoAt > 0 && repoAt < listAt, 'the rules are asked for before the list is awaited');
  assert.ok(load.indexOf('hiddenRepo = repo;') < load.indexOf('syncSelection();'));
});

test('hiding goes through the backend and never through git', () => {
  assert.match(storeSrc, /App\.AddDiffHiddenRule\(/);
  assert.match(storeSrc, /App\.RemoveDiffHiddenRules\(/);
  assert.match(storeSrc, /App\.GetDiffHiddenRules\(/);
  for (const src of [diffSrc, storeSrc]) {
    assert.doesNotMatch(src, /assume-unchanged|skip-worktree|\.gitignore'/);
    assert.doesNotMatch(src, /localStorage\.setItem\([^)]*[Hh]idden/);
  }
});

test('the file menu is the shared kind: portalled, kept on screen, one at a time', () => {
  const menu = diffSrc.slice(diffSrc.indexOf('class="context-menu diff-file-menu"'));
  const tag = menu.slice(0, menu.indexOf('>'));
  assert.match(tag, /use:portal/);
  assert.match(tag, /use:menuPosition=\{\{ x: menu\.x, y: menu\.y \}\}/);
  assert.match(diffSrc, /claimMenu\(closeFileMenu\)/);
  assert.match(diffSrc, /releaseMenu\(closeFileMenu\)/);
  // Every file row opens it, in both the tree and the flat list.
  assert.equal((diffSrc.match(/on:contextmenu=\{\(e\) => openFileMenu\(e, file\.path, 'file', file\.status\)\}/g) || []).length, 2);
  assert.match(diffSrc, /on:contextmenu=\{\(e\) => openFileMenu\(e, row\.path, 'dir'\)\}/);
});

test('the menu jumps to the file and to its folder in the file browser', () => {
  const menu = diffSrc.slice(diffSrc.indexOf('class="context-menu diff-file-menu"'));
  assert.match(menu, /data-action="show-in-files"\s+disabled=\{menu\.status === 'deleted'\}/);
  assert.match(menu, /showFileInFiles\(menu\.path\)/);
  assert.match(menu, /showFolderInFiles\(folder\)/);
  assert.match(diffSrc, /function showFolderInFiles\(dir: string\) \{\s*closeFileMenu\(\);\s*requestFolderJump\(dir\.replace/);
  assert.match(diffSrc, /function showFileInFiles\(path: string\) \{\s*closeFileMenu\(\);\s*openFileInBrowser\(path\);/);
  // A file at the root has no folder item.
  assert.match(menu, /\{#if folder\}\s*<button[^>]*\s+data-action="show-folder-in-files"/);
});

test('every new string is in every language', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const keys = [
    'diff.filterSkipped', 'diff.hidden.tabHint', 'diff.hidden.hint', 'diff.hidden.hide',
    'diff.hidden.hideFolder', 'diff.hidden.hidePattern', 'diff.hidden.showAgain',
    'diff.hidden.showAgainRule', 'diff.hidden.patternPlaceholder', 'diff.hidden.add',
    'diff.hidden.ruleCount', 'diff.hidden.none', 'diff.hidden.allHidden', 'diff.hidden.notCounted',
    'diff.menu.showInFiles', 'diff.menu.showFolderInFiles', 'diff.menu.deletedNotInFiles',
    'error.diffHiddenInvalidRule', 'error.diffHiddenTooManyRules', 'error.diffHiddenNoRepository',
  ];
  const locales = readdirSync(dir).filter((name) => name.endsWith('.json'));
  assert.equal(locales.length, 20);
  for (const name of locales) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of keys) {
      assert.equal(typeof strings[key], 'string', `${name} lacks ${key}`);
      assert.ok(strings[key].trim(), `${name} has an empty ${key}`);
    }
    for (const [key, slot] of [['diff.hidden.hideFolder', '{folder}'], ['diff.hidden.hidePattern', '{pattern}'],
      ['diff.hidden.showAgainRule', '{rules}'], ['diff.hidden.notCounted', '{count}']]) {
      assert.ok(strings[key].includes(slot), `${name}: ${key} lost ${slot}`);
    }
  }
  const en = JSON.parse(readFileSync(new URL('en.json', dir), 'utf8'));
  const hu = JSON.parse(readFileSync(new URL('hu.json', dir), 'utf8'));
  assert.equal(en['diff.filterSkipped'], 'Skipped');
  assert.equal(hu['diff.filterSkipped'], 'Kihagyott');
});
