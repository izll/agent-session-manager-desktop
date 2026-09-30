import { test, expect } from '@playwright/test';

/**
 * Hiding files from the diff view — the view only, nothing in git.
 *
 * The real diff component, with the backend mocked: a hidden file leaves All
 * and its status tab, turns up under Skipped with a count, can still be opened
 * there, and comes back with "show again".
 */

async function open(page, query = '') {
  await page.goto(`/tests/browser/diff-find-fixture.html?component=diff&extra=1&file=modified.txt${query}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await expect(page.locator('.selected-name')).toHaveText('modified.txt', { timeout: 15_000 });
}

const tab = (page, filter) => page.locator(`.status-filter[data-filter="${filter}"]`);
const count = (page, filter) => tab(page, filter).locator('.status-filter-count');
const row = (page, path) => page.locator(`.file-list .file-row[data-path="${path}"]`);
const menuItem = (page, action) => page.locator(`.diff-file-menu [data-action="${action}"]`);

test('hide a file: it leaves All and Added, and Skipped lists it until shown again', async ({ page }) => {
  await open(page);
  await tab(page, 'all').click();
  await expect(count(page, 'all')).toHaveText('6');
  await expect(count(page, 'added')).toHaveText('3');
  await expect(count(page, 'skipped')).toHaveText('0');

  await row(page, 'added.txt').click({ button: 'right' });
  await menuItem(page, 'hide').click();
  await expect(page.locator('.diff-file-menu')).toHaveCount(0);

  await expect(row(page, 'added.txt')).toHaveCount(0);
  await expect(count(page, 'all')).toHaveText('5');
  await expect(count(page, 'added')).toHaveText('2');
  await expect(count(page, 'skipped')).toHaveText('1');
  // The header counts what is under review.
  await expect(page.locator('.file-pane-header')).toContainText('5');
  expect(await page.evaluate(() => window.diffHidden.calls())).toEqual([['add', 'added.txt']]);

  await tab(page, 'added').click();
  await expect(row(page, 'added.txt')).toHaveCount(0);
  await expect(row(page, 'gen/out.txt')).toHaveCount(1);

  // Skipped: the file is there, and can still be read.
  await tab(page, 'skipped').click();
  await expect(page.locator('.file-list .file-row')).toHaveCount(1);
  await expect(page.locator('.selected-name')).toHaveText('added.txt');
  await expect(page.locator(':is(.diff-line, .sbs-line)', { hasText: /line 0$/ }).first()).toBeVisible();

  await row(page, 'added.txt').locator('.show-again').click();
  await expect(count(page, 'skipped')).toHaveText('0');
  await tab(page, 'all').click();
  await expect(row(page, 'added.txt')).toHaveCount(1);
  await expect(count(page, 'all')).toHaveText('6');
  expect(await page.evaluate(() => window.diffHidden.rules())).toEqual([]);
});

test('hiding the open file moves the selection off it; a row click still just selects', async ({ page }) => {
  await open(page);
  await tab(page, 'all').click();
  // No hide button sits in the row to catch a plain click.
  await row(page, 'deleted.txt').click();
  await expect(page.locator('.selected-name')).toHaveText('deleted.txt');
  await expect(count(page, 'skipped')).toHaveText('0');

  await row(page, 'deleted.txt').click({ button: 'right' });
  await menuItem(page, 'hide').click();
  await expect(row(page, 'deleted.txt')).toHaveCount(0);
  await expect(page.locator('.selected-name')).not.toHaveText('deleted.txt');
  await expect(count(page, 'skipped')).toHaveText('1');
  // The tab of a status with nothing left on show goes away.
  await expect(tab(page, 'deleted')).toHaveCount(0);
});

test('hide a folder and a pattern; each is listed under Skipped with its own show-again', async ({ page }) => {
  await open(page);
  await tab(page, 'all').click();

  await row(page, 'gen/out.txt').click({ button: 'right' });
  await expect(menuItem(page, 'hide-folder')).toContainText('gen/');
  await menuItem(page, 'hide-folder').click();
  await expect(row(page, 'gen/out.txt')).toHaveCount(0);
  await expect(row(page, 'gen/map.txt')).toHaveCount(0);
  await expect(count(page, 'skipped')).toHaveText('2');

  // A pattern typed into the Skipped tab.
  await tab(page, 'skipped').click();
  await page.locator('.skipped-input').fill('*.lock');
  await page.locator('.skipped-input').press('Enter');
  await expect(count(page, 'skipped')).toHaveText('3');
  await expect(page.locator('.skipped-input')).toHaveValue('');
  await expect(page.locator('.skipped-rule[data-rule="gen/"] .skipped-rule-count')).toHaveText('2');
  await expect(page.locator('.skipped-rule[data-rule="*.lock"] .skipped-rule-count')).toHaveText('1');
  await expect(row(page, 'src/app.lock')).toHaveCount(1);

  await page.locator('.skipped-rule[data-rule="gen/"] .show-again').click();
  await expect(count(page, 'skipped')).toHaveText('1');
  await expect(page.locator('.skipped-rule[data-rule="gen/"]')).toHaveCount(0);
  expect(await page.evaluate(() => window.diffHidden.rules())).toEqual(['*.lock']);
});

test('rules the backend already holds apply as the diff opens', async ({ page }) => {
  await open(page, '&hidden=gen/,deleted.txt');
  await expect(count(page, 'skipped')).toHaveText('3');
  await tab(page, 'all').click();
  await expect(count(page, 'all')).toHaveText('3');
  await expect(row(page, 'deleted.txt')).toHaveCount(0);
  await expect(tab(page, 'deleted')).toHaveCount(0);
});

test('stepping through changes passes over hidden files', async ({ page }) => {
  await open(page, '&hidden=gen/,src/,deleted.txt');
  await tab(page, 'all').click();
  const seen = new Set();
  for (let press = 0; press < 16; press++) {
    await page.keyboard.press('Control+F7');
    await page.waitForTimeout(60);
    seen.add(await page.locator('.selected-name').textContent());
  }
  expect([...seen].sort()).toEqual(['added.txt', 'modified.txt']);
});

test('the menu jumps to the file, and to its folder, in the file browser', async ({ page }) => {
  await open(page);
  await tab(page, 'all').click();

  await row(page, 'gen/out.txt').click({ button: 'right' });
  await menuItem(page, 'show-folder-in-files').click();
  expect(await page.evaluate(() => window.diffHidden.fileJump())).toEqual({ path: 'gen', folder: true });

  await row(page, 'gen/out.txt').click({ button: 'right' });
  await menuItem(page, 'show-in-files').click();
  expect(await page.evaluate(() => window.diffHidden.fileJump())).toMatchObject({ path: 'gen/out.txt' });

  // At the root there is no folder to show.
  await row(page, 'modified.txt').click({ button: 'right' });
  await expect(menuItem(page, 'show-in-files')).toBeEnabled();
  await expect(menuItem(page, 'show-folder-in-files')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(page.locator('.diff-file-menu')).toHaveCount(0);

  // A deleted file is not on disk to be shown.
  await row(page, 'deleted.txt').click({ button: 'right' });
  await expect(menuItem(page, 'show-in-files')).toBeDisabled();
});

test('show all again: every rule goes after a confirmation, and every file is back', async ({ page }) => {
  await open(page, '&hidden=gen/,deleted.txt');
  await tab(page, 'skipped').click();
  await expect(count(page, 'skipped')).toHaveText('3');
  const button = page.locator('.skipped-panel .show-all');
  await expect(button).toBeVisible();

  // Cancel leaves everything as it was.
  await button.click();
  const dialog = page.getByRole('dialog');
  // Two rules, three files.
  await expect(dialog).toContainText('2');
  await expect(dialog).toContainText('3');
  await dialog.getByRole('button', { name: /Cancel|Mégse/ }).click();
  await expect(count(page, 'skipped')).toHaveText('3');

  await button.click();
  await page.getByRole('dialog').getByRole('button', { name: /Show all again/ }).click();
  await expect(count(page, 'skipped')).toHaveText('0');
  await expect(button).toHaveCount(0);
  expect(await page.evaluate(() => window.diffHidden.rules())).toEqual([]);
  await tab(page, 'all').click();
  await expect(count(page, 'all')).toHaveText('6');
});
