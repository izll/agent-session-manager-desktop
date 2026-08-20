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

test('ConfirmDialog remains visible when its owning view is hidden', async ({ page }) => {
  await page.goto('/tests/browser/confirm-portal-fixture.html');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  expect(await dialog.evaluate((node) => node.parentElement === document.body)).toBe(true);
  await expect(page.getByRole('button', { name: 'Keep editing' })).toBeVisible();
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

test('TaskPanel keeps trailing metadata inside the realistic 300-320px main panel', async ({ page }) => {
  await page.goto('/tests/browser/task-panel-fixture.html');
  const combined = page.locator('.task-item').filter({ hasText: 'Minden metaadat' });
  for (const width of [320, 300]) {
    await page.locator('#fixture').evaluate((fixture, value) => { fixture.style.width = `${value}px`; }, width);
    const geometry = await combined.evaluate((item) => {
      const row = item.querySelector('.task-title-row').getBoundingClientRect();
      const status = item.querySelector('.status-badge').getBoundingClientRect();
      const priority = item.querySelector('.priority-badge').getBoundingClientRect();
      return { rowLeft: row.left, rowRight: row.right, statusLeft: status.left, priorityRight: priority.right };
    });
    expect(geometry.statusLeft).toBeGreaterThanOrEqual(geometry.rowLeft - 1);
    expect(geometry.priorityRight).toBeLessThanOrEqual(geometry.rowRight + 1);
  }
});

test('TaskPanel closes stale context menus and edit modals on session change', async ({ page }) => {
  await page.goto('/tests/browser/task-panel-fixture.html');
  await page.locator('.task-item').first().click({ button: 'right' });
  await expect(page.locator('.context-menu')).toBeVisible();
  await page.evaluate(() => window.taskPanelFixture.select('other-session', [{
    id: '4', title: 'OTHER SESSION ID 4', description: 'keep', details: 'keep', status: 'pending',
    priority: 'low', tags: [], dependencies: [], subtasks: [],
  }]));
  await expect(page.getByText('OTHER SESSION ID 4')).toBeVisible();
  await expect(page.locator('.context-menu')).toHaveCount(0);

  await page.locator('.task-item').first().click({ button: 'right' });
  await page.locator('.context-menu button').filter({ hasText: /Edit|Szerkeszt/ }).click();
  await expect(page.locator('.dialog-overlay')).toBeVisible();
  await page.evaluate(() => window.taskPanelFixture.select('third-session', [{
    id: '4', title: 'THIRD SESSION ID 4', description: 'keep', details: 'keep', status: 'pending',
    priority: 'low', tags: [], dependencies: [], subtasks: [],
  }]));
  await expect(page.locator('.dialog-overlay')).toHaveCount(0);
  expect(await page.evaluate(() => window.taskPanelFixture.updates())).toEqual([]);
});

test('a delayed TaskPanel save cannot close or overwrite a newer session modal', async ({ page }) => {
  await page.goto('/tests/browser/task-panel-fixture.html?delayUpdate=1');
  await page.locator('.task-item').first().click({ button: 'right' });
  await page.locator('.context-menu button').filter({ hasText: /Edit|Szerkeszt/ }).click();
  const firstTitle = page.locator('.dialog-content input[type="text"]').first();
  await firstTitle.fill('delayed session A edit');
  await page.locator('.dialog-content .btn-primary').click();
  await expect.poll(() => page.evaluate(() => window.taskPanelFixture.updates().length)).toBe(1);

  await page.evaluate(() => window.taskPanelFixture.select('new-session', [{
    id: '4', title: 'NEW SESSION TASK', description: '', details: '', status: 'pending',
    priority: 'low', tags: [], dependencies: [], subtasks: [],
  }]));
  await expect(page.getByText('NEW SESSION TASK')).toBeVisible();
  await page.locator('.task-item').first().click({ button: 'right' });
  await page.locator('.context-menu button').filter({ hasText: /Edit|Szerkeszt/ }).click();
  const newTitle = page.locator('.dialog-content input[type="text"]').first();
  await newTitle.fill('new session draft survives');

  await page.evaluate(() => window.taskPanelFixture.resolveUpdates());
  await expect(newTitle).toBeVisible();
  await expect(newTitle).toHaveValue('new session draft survives');
});

test('TaskPanel does not offer AI-only actions after MCP falls back to local tasks', async ({ page }) => {
  await page.goto('/tests/browser/task-panel-fixture.html?fallback=1');
  await expect(page.locator('.task-item')).toHaveCount(4);
  await page.getByRole('button', { name: /Add Task|Feladat hozzáadása/ }).click();
  await expect(page.getByRole('button', { name: /AI Generated|AI által generált/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Expand All|Összes kibontása/ })).toHaveCount(0);
});

test('Notes preserves a per-target draft after save failure and fails closed on load failure', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.goto('/tests/browser/notes-fixture.html');

  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('saved A', { timeout: 15000 });
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

test('Notes blocks a destructive action for a failed background draft', async ({ page }) => {
  await page.goto('/tests/browser/notes-fixture.html');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('saved A', { timeout: 15000 });
  await textarea.fill('draft A survives quit');
  await page.evaluate(() => window.notesFixture.select('notes-b'));
  await expect(textarea).toHaveValue('saved B');
  await expect.poll(() => page.evaluate(() => window.notesFixture.failedSaves())).toBe(1);

  await page.evaluate(() => window.notesFixture.attemptDestructive());
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  expect(await dialog.evaluate((node) => node.parentElement === document.body)).toBe(true);
  await page.getByRole('button', { name: /Keep editing|Szerkesztés folytatása/ }).click();
  await expect(page.locator('body')).toHaveAttribute('data-destructive', 'false');

  await page.evaluate(() => window.notesFixture.attemptDestructive());
  await page.getByRole('button', { name: /Discard changes|Módosítások elvetése/ }).click();
  await expect(page.locator('body')).toHaveAttribute('data-destructive', 'true');
});
