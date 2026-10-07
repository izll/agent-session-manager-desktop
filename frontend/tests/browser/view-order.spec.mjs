import { test, expect } from '@playwright/test';

/**
 * The view bar's order: set for every tab in Settings, and per tab from the
 * tab's menu, where it can be handed back to the global one.
 */
async function open(page) {
  await page.goto('/tests/browser/project-layout-fixture.html?dictation=0');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
  await expect(page.locator('.view-tabs-left [data-view]')).toHaveCount(5);
}

const shownOrder = (page) => page.locator('.view-tabs-left [data-view]')
  .evaluateAll((tabs) => tabs.map((tab) => tab.getAttribute('data-view')));
const fixture = (page, name, ...args) =>
  page.evaluate(([n, a]) => window.projectLayoutFixture[n](...a), [name, args]);

test('the global order sets the bar, and a tab\'s own wins over it', async ({ page }) => {
  await open(page);
  expect(await shownOrder(page)).toEqual(['terminal', 'notes', 'tasks', 'browser', 'diff']);

  await fixture(page, 'setGlobalViewOrder', ['diff', 'browser']);
  await expect.poll(() => shownOrder(page)).toEqual(['diff', 'browser', 'terminal', 'notes', 'tasks']);

  await fixture(page, 'setTabViewOrder', ['notes', 'terminal', 'tasks', 'browser', 'diff']);
  await expect.poll(() => shownOrder(page)).toEqual(['notes', 'terminal', 'tasks', 'browser', 'diff']);

  // An empty order of its own follows the global one again.
  await fixture(page, 'setTabViewOrder', []);
  await expect.poll(() => shownOrder(page)).toEqual(['diff', 'browser', 'terminal', 'notes', 'tasks']);
});

test('the view keys step in the order shown', async ({ page }) => {
  await open(page);
  await fixture(page, 'setGlobalViewOrder', ['browser', 'terminal', 'notes', 'tasks', 'diff']);
  await expect.poll(() => shownOrder(page)).toEqual(['browser', 'terminal', 'notes', 'tasks', 'diff']);
  await page.locator('.view-tabs-left [data-view="terminal"]').click();
  await page.keyboard.press('Control+Shift+PageUp');
  await expect(page.locator('.view-tabs-left [data-view="browser"]')).toHaveClass(/active/);
});

test('a tab gets an order of its own from its menu, and gives it back @webkit', async ({ page }) => {
  await open(page);
  await page.locator('.tab').first().click({ button: 'right' });
  await page.locator('[data-menu-action="view-order"]').click();
  const dialog = page.locator('[data-dialog="tab-view-order"]');
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('.source')).toHaveAttribute('data-source', 'global');
  await expect(dialog.locator('[data-action="use-global"]')).toBeDisabled();

  // Diff to the top, one step at a time.
  for (let i = 0; i < 4; i++) {
    await dialog.locator('[data-view="diff"] [data-move="up"]').click();
  }
  await expect.poll(() => shownOrder(page)).toEqual(['diff', 'terminal', 'notes', 'tasks', 'browser']);
  await expect(dialog.locator('.source')).toHaveAttribute('data-source', 'tab');
  expect((await fixture(page, 'viewOrderCalls')).at(-1)).toEqual(
    ['same-session', 0, ['diff', 'terminal', 'notes', 'tasks', 'browser']]);

  await dialog.locator('[data-action="use-global"]').click();
  await expect.poll(() => shownOrder(page)).toEqual(['terminal', 'notes', 'tasks', 'browser', 'diff']);
  expect((await fixture(page, 'viewOrderCalls')).at(-1)).toEqual(['same-session', 0, []]);
  await expect(dialog.locator('.source')).toHaveAttribute('data-source', 'global');
});
