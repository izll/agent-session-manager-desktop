import { test, expect } from '@playwright/test';

/**
 * A jump from the diff opened the file and its folders in the tree, but did
 * not scroll there: in a long tree the row was open somewhere below the fold.
 * A folder can be jumped to as well — opened out, marked and scrolled to,
 * without opening a file.
 */
async function open(page) {
  await page.goto('/tests/browser/file-jump-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
  await expect(page.locator('[data-tree-path="d00"]')).toBeVisible();
  // The targets start out of sight.
  await expect(page.locator('[data-tree-path="zz"]')).not.toBeInViewport();
}

test('a jump to a file scrolls the tree to it', async ({ page }) => {
  await open(page);
  await page.evaluate(() => window.fileJumpFixture.requestFileJump('zz/deep/target.txt', 1));
  const row = page.locator('[data-tree-path="zz/deep/target.txt"]');
  await expect(row).toHaveClass(/selected/);
  await expect(row).toBeInViewport();
  await expect(page.locator('.cm-content')).toContainText('content of zz/deep/target.txt');
});

test('a jump to a folder opens it, marks it and scrolls to it, opening no file', async ({ page }) => {
  await open(page);
  await page.evaluate(() => window.fileJumpFixture.requestFolderJump('zz/deep/inner'));
  const row = page.locator('[data-tree-path="zz/deep/inner"]');
  await expect(row).toHaveClass(/selected/);
  await expect(row).toHaveAttribute('aria-expanded', 'true');
  await expect(row).toBeInViewport();
  await expect(page.locator('[data-tree-path="zz/deep/inner/x.txt"]')).toBeVisible();
  await expect(page.locator('.cm-content')).toHaveCount(0);

  // Choosing a file takes the mark off the folder.
  await page.locator('[data-tree-path="zz/deep/inner/x.txt"]').click();
  await expect(row).not.toHaveClass(/selected/);
});
