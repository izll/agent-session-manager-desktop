import { test, expect } from '@playwright/test';

/**
 * Ctrl+Shift+PgDn / Ctrl+Shift+PgUp step through the tab's views — terminal,
 * notes, tasks, files, diff — as Ctrl+PgUp/PgDn step through its tabs.
 */
async function open(page) {
  await page.goto('/tests/browser/project-layout-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
}

const activeView = (page) => page.evaluate(() => {
  // The open diff marks the view it covers as "behind" it.
  if (document.querySelector('.view-tabs-left .view-tab.behind-diff')) return 'diff';
  const tabs = [...document.querySelectorAll('.view-tabs-left .view-tab')];
  return tabs.findIndex((tab) => tab.classList.contains('active'));
});

test('the view keys step through the views and wrap round', async ({ page }) => {
  await open(page);
  await expect.poll(() => activeView(page)).toBe(0); // terminal
  const seen = [];
  for (let i = 0; i < 5; i++) {
    await page.keyboard.press('Control+Shift+PageDown');
    seen.push(await activeView(page));
  }
  // notes, tasks, files, diff, and round to the terminal.
  expect(seen).toEqual([1, 2, 3, 'diff', 0]);

  await page.keyboard.press('Control+Shift+PageUp');
  await expect.poll(() => activeView(page)).toBe('diff');
  await page.keyboard.press('Control+Shift+PageUp');
  await expect.poll(() => activeView(page)).toBe(3);
});
