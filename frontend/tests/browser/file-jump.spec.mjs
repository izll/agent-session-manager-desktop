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

// In the app the full diff replaces the view the browser lives in, so the
// browser is mounted by the very switch the jump asked for. It handled the
// jump before its root was listed, read nothing, and cleared it on the way:
// no file opened and the tree stayed put.
for (const [what, jump, rowPath] of [
  ['file', () => window.fileJumpFixture.requestFileJump('zz/deep/target.txt', 1), 'zz/deep/target.txt'],
  ['folder', () => window.fileJumpFixture.requestFolderJump('zz/deep/inner'), 'zz/deep/inner'],
]) {
  test(`a ${what} jump made before the browser is mounted still lands`, async ({ page }) => {
    await page.goto('/tests/browser/file-jump-fixture.html?late=1');
    await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
    await page.evaluate(jump);
    await page.evaluate(() => window.fileJumpFixture.mountBrowser());
    const row = page.locator(`[data-tree-path="${rowPath}"]`);
    await expect(row).toHaveClass(/selected/);
    await expect(row).toBeInViewport();
    if (what === 'file') await expect(page.locator('.cm-content')).toContainText('content of zz/deep/target.txt');
  });
}
