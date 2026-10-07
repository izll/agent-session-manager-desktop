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

const dragView = async (page, view, onto, side) => {
  const target = page.locator(`.view-tabs-left [data-view="${onto}"]`);
  const box = await target.boundingBox();
  await page.locator(`.view-tabs-left [data-view="${view}"]`).dragTo(target, {
    targetPosition: { x: side === 'before' ? 3 : box.width - 3, y: box.height / 2 },
  });
};

const dropDialog = (page) => page.locator('[data-dialog="view-order-drop"]');
const DEFAULT = ['terminal', 'notes', 'tasks', 'browser', 'diff'];

test('a drop asks what it changes, and "every tab" sets the global order @webkit', async ({ page }) => {
  await open(page);
  await dragView(page, 'browser', 'terminal', 'before');
  await expect(dropDialog(page)).toBeVisible();
  // Nothing is stored before the answer.
  expect(await shownOrder(page)).toEqual(DEFAULT);
  await dropDialog(page).locator('[data-target="global"]').click();
  await expect(dropDialog(page)).toHaveCount(0);
  await expect.poll(() => shownOrder(page)).toEqual(['browser', 'terminal', 'notes', 'tasks', 'diff']);
  expect((await fixture(page, 'settingsSaves')).at(-1)[0].viewOrder)
    .toEqual(['browser', 'terminal', 'notes', 'tasks', 'diff']);
  expect(await fixture(page, 'viewOrderCalls')).toEqual([]);

  // Past the last one's middle is after it.
  await dragView(page, 'terminal', 'diff', 'after');
  await dropDialog(page).locator('[data-target="global"]').click();
  await expect.poll(() => shownOrder(page)).toEqual(['browser', 'notes', 'tasks', 'diff', 'terminal']);
});

test('"this tab only" gives the tab an order of its own', async ({ page }) => {
  await open(page);
  await dragView(page, 'diff', 'terminal', 'before');
  await dropDialog(page).locator('[data-target="tab"]').click();
  await expect.poll(() => shownOrder(page)).toEqual(['diff', 'terminal', 'notes', 'tasks', 'browser']);
  expect((await fixture(page, 'viewOrderCalls')).at(-1)).toEqual(
    ['same-session', 0, ['diff', 'terminal', 'notes', 'tasks', 'browser']]);
  expect(await fixture(page, 'settingsSaves')).toEqual([]);
});

test('"every tab" in a tab with its own order also hands it back to the global one', async ({ page }) => {
  await open(page);
  await fixture(page, 'setTabViewOrder', ['notes', 'terminal', 'tasks', 'browser', 'diff']);
  await expect.poll(() => shownOrder(page)).toEqual(['notes', 'terminal', 'tasks', 'browser', 'diff']);
  await dragView(page, 'diff', 'notes', 'before');
  await expect(dropDialog(page).locator('[data-target="global"]')).toContainText('gives up its own');
  await dropDialog(page).locator('[data-target="global"]').click();
  await expect.poll(() => shownOrder(page)).toEqual(['diff', 'notes', 'terminal', 'tasks', 'browser']);
  expect((await fixture(page, 'settingsSaves')).at(-1)[0].viewOrder)
    .toEqual(['diff', 'notes', 'terminal', 'tasks', 'browser']);
  expect((await fixture(page, 'viewOrderCalls')).at(-1)).toEqual(['same-session', 0, []]);
});

test('a remembered answer is saved and the next drop does not ask', async ({ page }) => {
  await open(page);
  await dragView(page, 'browser', 'terminal', 'before');
  await dropDialog(page).locator('[data-remember]').check();
  await dropDialog(page).locator('[data-target="tab"]').click();
  await expect.poll(async () => (await fixture(page, 'settingsSaves')).map((save) => save[0].viewOrderDrag))
    .toContain('tab');
  await expect.poll(() => shownOrder(page)).toEqual(['browser', 'terminal', 'notes', 'tasks', 'diff']);

  await dragView(page, 'diff', 'browser', 'before');
  await expect.poll(() => shownOrder(page)).toEqual(['diff', 'browser', 'terminal', 'notes', 'tasks']);
  await expect(dropDialog(page)).toHaveCount(0);
  expect((await fixture(page, 'viewOrderCalls')).at(-1)).toEqual(
    ['same-session', 0, ['diff', 'browser', 'terminal', 'notes', 'tasks']]);
});

test('cancelling the question changes nothing', async ({ page }) => {
  await open(page);
  await dragView(page, 'browser', 'terminal', 'before');
  await page.keyboard.press('Escape');
  await expect(dropDialog(page)).toHaveCount(0);
  expect(await shownOrder(page)).toEqual(DEFAULT);
  expect(await fixture(page, 'settingsSaves')).toEqual([]);
  expect(await fixture(page, 'viewOrderCalls')).toEqual([]);
});

test('dropping a view where it already is asks nothing and changes nothing', async ({ page }) => {
  await open(page);
  await dragView(page, 'notes', 'tasks', 'before');
  await dragView(page, 'notes', 'notes', 'after');
  await expect(dropDialog(page)).toHaveCount(0);
  expect(await shownOrder(page)).toEqual(DEFAULT);
  expect(await fixture(page, 'settingsSaves')).toEqual([]);
  expect(await fixture(page, 'viewOrderCalls')).toEqual([]);
});
