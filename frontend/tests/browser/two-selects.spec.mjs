import { test, expect } from '@playwright/test';

/**
 * Opening one dropdown left another open beside it: in the project task
 * dialog the session list and the tab list spilled over each other. One is
 * open at a time.
 */
test('opening one dropdown closes the other', async ({ page }) => {
  await page.goto('/tests/browser/two-selects-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  const lists = page.locator('.select-dropdown');

  await page.locator('#first .select-trigger').click();
  await expect(lists).toHaveCount(1);
  await expect(lists.first()).toContainText('Session 3');

  await page.locator('#second .select-trigger').click();
  await expect(lists).toHaveCount(1);
  await expect(lists.first()).toContainText('Tab 3');

  await page.locator('#first .select-trigger').click();
  await expect(lists).toHaveCount(1);
  await expect(lists.first()).toContainText('Session 3');
});
