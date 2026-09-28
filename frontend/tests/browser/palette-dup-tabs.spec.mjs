import { test, expect } from '@playwright/test';

/**
 * An old store can hold several records on one tab index. The command palette
 * listed each under the same key, and a keyed list given two rows under one
 * key throws: the palette never appeared, its open flag stayed set, and every
 * shortcut after it — Ctrl+J included — did nothing.
 */
test('the palette opens on a session with several records on one tab index', async ({ page }) => {
  const errors = [];
  page.on('pageerror', (error) => errors.push(String(error)));
  await page.goto('/tests/browser/dialog-races-fixture.html?mode=palette&dupTabs=1');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });

  const input = page.getByRole('dialog').locator('input').first();
  await expect(input).toBeVisible();
  await input.fill('Dup tabs ›');
  // One row per index: the three records are one window.
  await expect(page.getByRole('dialog').getByText(/Dup tabs › /)).toHaveCount(2); // main tab + index 1
  expect(errors.filter((e) => /each_key_duplicate/.test(e))).toEqual([]);
});
