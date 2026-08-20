import { test, expect } from '@playwright/test';

test('a real Svelte component renders, portals, focuses and reacts in Chromium', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

  await page.goto('/tests/browser/select-fixture.html');
  await page.getByRole('button', { name: 'Alpha' }).click();

  const search = page.locator('.select-search');
  await expect(search).toBeFocused();
  await expect(page.locator('body > .select-dropdown')).toBeVisible();
  await search.fill('gam');
  await search.press('Enter');

  await expect(page.locator('body')).toHaveAttribute('data-selected', 'gamma');
  await expect(page.getByRole('button', { name: 'Gamma' })).toBeVisible();
  expect(pageErrors).toEqual([]);
});

test('TaskPanel keeps metadata on one right-aligned row with optional badges', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.goto('/tests/browser/task-panel-fixture.html');
  await expect(page.locator('.task-item')).toHaveCount(3);

  const geometry = await page.locator('.task-item').evaluateAll((items) => items.map((item) => {
    const title = item.querySelector('.task-title-row').getBoundingClientRect();
    const priority = item.querySelector('.priority-badge').getBoundingClientRect();
    const status = item.querySelector('.status-badge').getBoundingClientRect();
    return {
      rowHeight: title.height,
      priorityRight: priority.right,
      statusRight: status.right,
      priorityTop: priority.top,
      statusTop: status.top,
      statusHeight: status.height,
    };
  }));

  expect(Math.max(...geometry.map((row) => row.rowHeight))).toBeLessThanOrEqual(24);
  expect(new Set(geometry.map((row) => Math.round(row.priorityRight))).size).toBe(1);
  expect(new Set(geometry.map((row) => Math.round(row.statusRight))).size).toBe(1);
  for (const row of geometry) {
    expect(Math.abs(row.priorityTop - row.statusTop)).toBeLessThanOrEqual(1);
    expect(row.statusHeight).toBeLessThanOrEqual(20);
  }
  expect(pageErrors).toEqual([]);
});
