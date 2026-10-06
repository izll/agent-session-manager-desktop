import { test, expect } from '@playwright/test';

/**
 * The folder the diff shows: the tab's by default, or one the session chose —
 * a repository inside a folder that holds several. The header says which and
 * lets it be changed; a jump to the Files view, which still shows the tab's
 * folder, finds the file from there.
 */
async function open(page, query = '') {
  await page.goto(`/tests/browser/diff-find-fixture.html?component=diff&extra=1&file=modified.txt${query}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await expect(page.locator('.selected-name')).toHaveText('modified.txt', { timeout: 15_000 });
}

const chip = (page) => page.locator('[data-diff-folder]');
const folderItem = (page, action) => page.locator(`.diff-folder-menu [data-action="${action}"]`);
const calls = (page) => page.evaluate(() => window.diffHidden.diffDirCalls());

test('the header names the tab\'s folder, and another one can be chosen @webkit', async ({ page }) => {
  await open(page);
  await expect(chip(page)).toHaveText('repo');
  await expect(chip(page)).not.toHaveClass(/custom/);

  await chip(page).click();
  await expect(folderItem(page, 'session-folder')).toHaveAttribute('aria-checked', 'true');
  await folderItem(page, 'other-folder').click();

  await expect.poll(() => calls(page)).toEqual(['/repo/picked']);
  await expect(chip(page)).toHaveText('picked');
  await expect(chip(page)).toHaveClass(/custom/);
  await expect(page.locator('.diff-folder-menu')).toHaveCount(0);
});

test('a chosen folder can be left for the session\'s again', async ({ page }) => {
  await open(page, '&folder=inner');
  await expect(chip(page)).toHaveText('inner');
  await expect(chip(page)).toHaveClass(/custom/);

  await chip(page).click();
  await expect(folderItem(page, 'session-folder')).toHaveAttribute('aria-checked', 'false');
  await folderItem(page, 'session-folder').click();
  await expect.poll(() => calls(page)).toEqual(['']);
  await expect(chip(page)).toHaveText('repo');
});

test('Escape closes the folder menu', async ({ page }) => {
  await open(page);
  await chip(page).click();
  await expect(page.locator('.diff-folder-menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.diff-folder-menu')).toHaveCount(0);
});

// The Files view still shows the tab's folder, /repo; the diff shows
// /repo/inner. A file of the diff is found from there under inner/.
test('a jump to the Files view names the file as the Files view sees it', async ({ page }) => {
  await open(page, '&folder=inner');
  await page.locator('.file-list .file-row[data-path="added.txt"]').click({ button: 'right' });
  await page.locator('.diff-file-menu [data-action="show-in-files"]').click();
  await expect.poll(() => page.evaluate(() => window.diffHidden.fileJump()?.path)).toBe('inner/added.txt');
});

test('a tab with a folder of its own cannot have another chosen', async ({ page }) => {
  await open(page, '&locked=ownFolder');
  await expect(chip(page)).toBeDisabled();
  await expect(chip(page)).toHaveAttribute('title', /\/repo/);
});
