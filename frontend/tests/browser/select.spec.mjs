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
  await expect(page.locator('.task-item')).toHaveCount(4);

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

test('TaskPanel keeps trailing status and priority inside a 520px combined-metadata row', async ({ page }) => {
  await page.goto('/tests/browser/task-panel-fixture.html');
  await expect(page.locator('.task-item')).toHaveCount(4);
  await page.locator('#fixture').evaluate((fixture) => { fixture.style.width = '520px'; });

  const combined = page.locator('.task-item').filter({ hasText: 'Minden metaadat' });
  const geometry = await combined.evaluate((item) => {
    const row = item.querySelector('.task-title-row').getBoundingClientRect();
    const optional = item.querySelector('.optional-meta').getBoundingClientRect();
    const trailing = item.querySelector('.trailing-meta').getBoundingClientRect();
    const status = item.querySelector('.status-badge').getBoundingClientRect();
    const priority = item.querySelector('.priority-badge').getBoundingClientRect();
    return {
      rowHeight: row.height,
      rowRight: row.right,
      optionalRight: optional.right,
      trailingLeft: trailing.left,
      statusTop: status.top,
      priorityTop: priority.top,
      priorityRight: priority.right,
    };
  });

  expect(geometry.rowHeight).toBeLessThanOrEqual(24);
  expect(geometry.priorityRight).toBeLessThanOrEqual(geometry.rowRight + 1);
  expect(geometry.optionalRight).toBeLessThanOrEqual(geometry.trailingLeft + 1);
  expect(Math.abs(geometry.statusTop - geometry.priorityTop)).toBeLessThanOrEqual(1);
});

test('Notes preserves a per-target draft after save failure and fails closed on load failure', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.goto('/tests/browser/notes-fixture.html');

  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('saved A');
  await textarea.fill('draft A survives');
  await page.evaluate(() => window.notesFixture.select('notes-b'));
  await expect(textarea).toHaveValue('saved B');
  await page.evaluate(() => window.notesFixture.select('notes-a'));

  await expect(textarea).toHaveValue('draft A survives');
  await expect(page.locator('.notes-error')).toContainText('save refused');
  await expect(textarea).toBeEnabled();
  expect(await page.evaluate(() => window.notesFixture.stored('notes-a'))).toBe('saved A');

  await page.evaluate(() => window.notesFixture.select('notes-load-fails'));
  await expect(page.locator('.notes-error')).toContainText('load refused');
  await expect(textarea).toBeDisabled();
  expect(pageErrors).toEqual([]);
});
