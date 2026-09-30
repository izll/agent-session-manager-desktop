import { test, expect } from '@playwright/test';

/**
 * The branch menu and the push/pull panel of the git badge opened on top of
 * each other: opening the panel closed the menu, but not the other way round,
 * and the header's badge knew nothing of the status bar's. One is open at a
 * time now.
 */
async function open(page) {
  await page.goto('/tests/browser/git-badge-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}
const header = (page) => page.locator('#header');
const status = (page) => page.locator('#status');
const branchMenu = (page) => page.locator('.branch-menu');
const syncPanel = (page) => page.locator('.git-sync-panel');

test('the branch menu and the push panel close each other', async ({ page }) => {
  await open(page);
  await header(page).locator('.git-unpushed-badge').click();
  await expect(syncPanel(page)).toHaveCount(1);

  await header(page).locator('.git-branch-main').click();
  await expect(branchMenu(page)).toHaveCount(1);
  await expect(syncPanel(page)).toHaveCount(0);

  await header(page).locator('.git-unpushed-badge').click();
  await expect(syncPanel(page)).toHaveCount(1);
  await expect(branchMenu(page)).toHaveCount(0);
});

test('the header badge and the status bar badge do not open on top of each other', async ({ page }) => {
  await open(page);
  await header(page).locator('.git-unpushed-badge').click();
  await expect(syncPanel(page)).toHaveCount(1);
  await status(page).locator('.git-branch-main').click();
  await expect(branchMenu(page)).toHaveCount(1);
  await expect(syncPanel(page)).toHaveCount(0);
});
