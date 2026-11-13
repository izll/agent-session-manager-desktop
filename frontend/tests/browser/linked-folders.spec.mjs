import { test, expect } from '@playwright/test';

// Folders linked to a tab: listed in a window of their own, with each one's diff and
// files, read from that folder.
async function open(page, query = '') {
  await page.goto(`/tests/browser/linked-folders-fixture.html${query}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}
const calls = (page, method) => page.evaluate((m) => window.linkedFixture.calls().filter((c) => c.method === m).map((c) => c.args), method);

test('with nothing linked it says how to start, and a folder can be added @webkit', async ({ page }) => {
  await open(page);
  await expect(page.locator('.linked-empty')).toBeVisible();
  await page.locator('[data-link-folder]').click();
  await expect.poll(() => calls(page, 'AddTabLinkedFolder')).toEqual([['/work/lib', 'p1']]);
  await expect(page.locator('.folder-row.selected .folder-name')).toHaveText('lib');
  // Its diff opens at once, read from that folder.
  await expect.poll(() => calls(page, 'GetFullDiffFileList')).toContainEqual(['/work/lib']);
});

test('the files of a linked folder are its own', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  await page.getByRole('tab', { name: /Files|Fájlok/ }).click();
  await expect.poll(() => calls(page, 'ListSessionDirectory')).toContainEqual(['', '/work/lib']);
  await expect(page.locator('[data-linked-folders] .folder-view')).toContainText('README.md');
});

test('a folder that is not a repository opens on its files', async ({ page }) => {
  await open(page, '?linked=/work/plain');
  await expect(page.getByRole('tab', { name: /Diff/ })).toBeDisabled();
  await expect.poll(() => calls(page, 'ListSessionDirectory')).toContainEqual(['', '/work/plain']);
});

test('a folder that is gone is marked, and can be unlinked', async ({ page }) => {
  await open(page, '?linked=/work/gone');
  await expect(page.locator('.folder-row.missing')).toHaveCount(1);
  await expect(page.locator('.linked-empty')).toBeVisible();
  await page.locator('[data-unlink="/work/gone"]').click();
  await expect.poll(() => calls(page, 'RemoveTabLinkedFolder')).toEqual([['/work/gone']]);
  await expect(page.locator('.folder-row')).toHaveCount(0);
});

// Every view inside reads the selected tab: shown for another, it would mix them.
test('switching tabs closes it', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  await page.evaluate(() => window.linkedFixture.switchTab());
  await expect(page.locator('[data-linked-folders]')).toHaveCount(0);
});

// From the diff to the file, in this window, even when the folder lists slowly.
test('a file of the diff opens in the files beside it', async ({ page }) => {
  await open(page, '?linked=/work/lib&slow');
  const row = page.locator('[data-linked-folders] .file-list .file-row[data-path="changed.txt"]');
  await row.click({ button: 'right' });
  await page.locator('.diff-file-menu [data-action="show-in-files"]').click();
  await expect(page.getByRole('tab', { name: /Files|Fájlok/ })).toHaveAttribute('aria-selected', 'true');
  await expect.poll(() => calls(page, 'ReadSessionDirectoryFile')).toContainEqual(['changed.txt', '/work/lib']);
});

// An edit is not thrown away by closing the window, nor by switching away from it.
test('unsaved edits are asked about before the window closes', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  await page.getByRole('tab', { name: /Files|Fájlok/ }).click();
  await page.locator('[data-tree-path="README.md"]').click();
  await page.getByRole('button', { name: /^(Edit|Szerkesztés)$/ }).click();
  const editor = page.locator('[data-linked-folders] .editor:not(.read) .cm-content');
  await expect(editor).toContainText('content of README.md');
  await editor.click();
  await page.keyboard.type('changed ');

  await page.getByRole('tab', { name: /^Diff$/ }).click();
  await page.getByRole('button', { name: /Keep editing|Szerkesztés folytatása/ }).click();
  await expect(page.getByRole('tab', { name: /Files|Fájlok/ })).toHaveAttribute('aria-selected', 'true');

  await page.locator('[data-linked-folders] .dialog-header button').last().click();
  await page.getByRole('button', { name: /Keep editing|Szerkesztés folytatása/ }).click();
  await expect(page.locator('[data-linked-folders]')).toHaveCount(1);
  await expect(editor).toContainText('changed');

  await page.locator('[data-linked-folders] .dialog-header button').last().click();
  await page.getByRole('button', { name: /Discard changes|Változások elvetése/ }).click();
  await expect(page.locator('[data-linked-folders]')).toHaveCount(0);
});

test('a menu open over the diff closes before the window does', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  await page.locator('[data-linked-folders] .file-list .file-row[data-path="changed.txt"]').click({ button: 'right' });
  await expect(page.locator('.diff-file-menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.diff-file-menu')).toHaveCount(0);
  await expect(page.locator('[data-linked-folders]')).toHaveCount(1);
});

test('on a server no folder can be linked yet, and it says why', async ({ page }) => {
  await open(page, '?server=srv1');
  await expect(page.locator('[data-link-folder]')).toBeDisabled();
  await expect(page.locator('.linked-note')).toBeVisible();
});

// Big diffs want the whole screen; the choice is kept for the next opening.
test('the window can be maximised and restored', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  const dialog = page.locator('[data-linked-folders] .linked-dialog');
  const toggle = page.locator('[data-linked-folders] [data-maximise]');
  const viewport = page.viewportSize();
  await toggle.click();
  await expect(dialog).toHaveClass(/maximised/);
  await expect.poll(async () => (await dialog.boundingBox()).width).toBe(viewport.width);
  await expect(toggle).toHaveAttribute('aria-label', /Restore|Eredeti méret/);

  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await expect(dialog).toHaveClass(/maximised/);
  await toggle.click();
  await expect(dialog).not.toHaveClass(/maximised/);
  await expect.poll(async () => (await dialog.boundingBox()).width).toBeLessThan(viewport.width - 20);
});

// A linked repository's commits, in the history window, read from that folder.
test('a linked folder\'s commit history opens', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  await page.locator('[data-linked-history]').click();
  await expect.poll(() => calls(page, 'GetGitHistory')).toContainEqual(['/work/lib']);
  await expect(page.locator('.commit-subject', { hasText: 'First commit' })).toBeVisible();
  // Over this window, not behind it.
  const onTop = await page.evaluate(() => {
    const hit = document.elementFromPoint(innerWidth / 2, innerHeight / 2);
    return !!hit?.closest('.history-dialog');
  });
  expect(onTop).toBe(true);
});

// The icon sits on the label's line, centred like the text beside it.
test('the history button\'s icon is centred on its label @webkit', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  const centres = await page.locator('[data-linked-history]').evaluate((button) => {
    const icon = button.querySelector('svg').getBoundingClientRect();
    const range = document.createRange();
    const text = [...button.childNodes].find((n) => n.nodeType === 3 && n.textContent.trim());
    range.selectNodeContents(text);
    const label = range.getBoundingClientRect();
    const box = button.getBoundingClientRect();
    return { icon: icon.top + icon.height / 2, label: label.top + label.height / 2, box: box.top + box.height / 2 };
  });
  // A pixel below the line's middle, on the lowercase letters, and no more.
  expect(centres.icon - centres.label).toBeGreaterThanOrEqual(0);
  expect(centres.icon - centres.label).toBeLessThanOrEqual(1.5);
  expect(Math.abs(centres.label - centres.box)).toBeLessThanOrEqual(1);
});

test('a folder that is not a repository has no history to open', async ({ page }) => {
  await open(page, '?linked=/work/plain');
  await expect(page.locator('[data-linked-history]')).toBeDisabled();
});

// A path reads as written, its leading slash first, even where it is cut short.
test('a folder\'s path reads left to right', async ({ page }) => {
  await open(page, '?linked=/work/lib');
  const path = page.locator('.folder-row .folder-path');
  await expect(path).toHaveText('/work/lib');
  const slashIsLeftmost = await path.evaluate((el) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    const text = walker.nextNode();
    const rect = (from) => {
      const range = document.createRange();
      range.setStart(text, from);
      range.setEnd(text, from + 1);
      return range.getBoundingClientRect().left;
    };
    return rect(0) < rect(text.length - 1);
  });
  expect(slashIsLeftmost).toBe(true);
});
