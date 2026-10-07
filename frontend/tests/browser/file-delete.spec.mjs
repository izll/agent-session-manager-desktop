import { test, expect } from '@playwright/test';

/**
 * A file deleted in the Files view is confirmed first, then gone at once, and
 * for ten seconds can be put back; after that the backend is told to let it go.
 */
async function open(page) {
  await page.goto('/tests/browser/file-delete-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
}

const calls = (page) => page.evaluate(() => window.fileDeleteFixture.calls());
const dialog = (page) => page.locator('.dialog-overlay');

test('the delete button asks first, and cancelling deletes nothing', async ({ page }) => {
  await open(page);
  await page.locator('[data-tree-path="a.txt"]').click();
  await page.locator('[data-action="delete-file"]').click();
  await expect(dialog(page)).toContainText('a.txt');
  await page.keyboard.press('Escape');
  await expect(dialog(page)).toHaveCount(0);
  expect(await calls(page)).toEqual([]);
  await expect(page.locator('[data-tree-path="a.txt"]')).toBeVisible();
});

test('a confirmed delete removes the file and Undo puts it back', async ({ page }) => {
  await open(page);
  await page.locator('[data-tree-path="a.txt"]').click();
  await page.locator('[data-action="delete-file"]').click();
  await dialog(page).getByRole('button', { name: 'Delete' }).click();

  await expect(page.locator('[data-tree-path="a.txt"]')).toHaveCount(0);
  await expect(page.locator('.selected-path')).toHaveCount(0);
  expect(await calls(page)).toEqual(['delete a.txt /repo/ project-a']);

  const toast = page.locator('.undo-toast');
  await expect(toast).toContainText('a.txt');
  await toast.getByRole('button', { name: /Undo/ }).click();
  await expect(page.locator('[data-tree-path="a.txt"]')).toBeVisible();
  // It was the open file, so it is open again.
  await expect(page.locator('.selected-path')).toContainText('a.txt');
  expect(await calls(page)).toEqual(['delete a.txt /repo/ project-a', 'restore token-1']);
});

test('Delete on a tree row deletes that file, and the offer ends after ten seconds', async ({ page }) => {
  await page.clock.install();
  await open(page);
  await page.locator('[data-tree-path="docs"]').click();
  const guide = page.locator('[data-tree-path="docs/guide.md"]');
  await guide.focus();
  await page.keyboard.press('Delete');
  await expect(dialog(page)).toContainText('guide.md');
  await dialog(page).getByRole('button', { name: 'Delete' }).click();
  await expect(guide).toHaveCount(0);
  await expect(page.locator('.undo-toast')).toBeVisible();

  await page.clock.runFor(9_000);
  await expect(page.locator('.undo-toast')).toBeVisible();
  expect(await calls(page)).toEqual(['delete docs/guide.md /repo/ project-a']);

  await page.clock.runFor(2_000);
  await expect(page.locator('.undo-toast')).toHaveCount(0);
  await expect.poll(() => calls(page)).toEqual(['delete docs/guide.md /repo/ project-a', 'discard token-1']);
});

test('dismissing the offer lets the file go', async ({ page }) => {
  await open(page);
  await page.locator('[data-tree-path="b.txt"]').click();
  await page.locator('[data-action="delete-file"]').click();
  await dialog(page).getByRole('button', { name: 'Delete' }).click();
  await page.locator('.undo-toast .close').click();
  await expect.poll(() => calls(page)).toEqual(['delete b.txt /repo/ project-a', 'discard token-1']);
});

test('a refused delete is reported and the file stays', async ({ page }) => {
  await open(page);
  await page.evaluate(() => window.fileDeleteFixture.failNextDelete('permission denied'));
  await page.locator('[data-tree-path="a.txt"]').click();
  await page.locator('[data-action="delete-file"]').click();
  await dialog(page).getByRole('button', { name: 'Delete' }).click();
  await expect.poll(() => page.evaluate(() => window.fileDeleteFixture.lastError())).toContain('permission denied');
  await expect(page.locator('[data-tree-path="a.txt"]')).toBeVisible();
  await expect(page.locator('.selected-path')).toContainText('a.txt');
  await expect(page.locator('.undo-toast')).toHaveCount(0);
});
