import { test, expect } from '@playwright/test';

// Moving tabs between sessions, from the UI: a tab dragged from the tab bar
// onto a session in the sidebar, the tab's menu, and the session's menu.
// Drag and drop is what browser engines differ in, so those run on WebKit
// (the app's webview) as well.
const WEBKIT = { tag: '@webkit' };

async function gotoFixture(page) {
  await page.goto('/tests/browser/tab-move-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await expect(tab(page, 'worker')).toBeVisible();
}

const tab = (page, name) => page.locator('.tab-bar .tab', { hasText: name });
const row = (page, name) => page.locator('.session-list .session-item', { hasText: name });
const fixture = (page, what) => page.evaluate((w) => window.tabMoveFixture[w](), what);

/** Holds a drag over a target without letting go, as a hand would. */
async function holdOver(page, source, target) {
  await source.hover();
  await page.mouse.down();
  const box = await target.boundingBox();
  await page.mouse.move(box.x + 10, box.y + box.height / 2, { steps: 4 });
  await page.mouse.move(box.x + 20, box.y + box.height / 2, { steps: 4 });
}

test('a tab dropped on a session moves there, and the view follows it', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await tab(page, 'worker').dragTo(row(page, 'Dest'));

  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MoveTabToSession', args: ['src', 1, 'dst', 'project-a'] },
  ]);
  await expect.poll(() => fixture(page, 'selected')).toEqual({ sessionId: 'dst', windowIdx: 5 });
  await expect.poll(() => fixture(page, 'notices')).toEqual(['“worker” moved to Dest']);
  await expect(row(page, 'Dest')).not.toHaveClass(/tab-drop-ok/);
});

test('a session that would refuse the tab says so and takes nothing', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await holdOver(page, tab(page, 'worker'), row(page, 'Remote'));
  await expect(row(page, 'Remote')).toHaveClass(/tab-drop-refused/);
  await expect(row(page, 'Remote')).toHaveAttribute('title', /cannot join a session on a server/);
  await page.mouse.up();

  // Over one that takes it, the row says that instead.
  await holdOver(page, tab(page, 'worker'), row(page, 'Other'));
  await expect(row(page, 'Other')).toHaveClass(/tab-drop-ok/);
  await expect(row(page, 'Remote')).not.toHaveClass(/tab-drop-refused/);
  await page.keyboard.press('Escape');
  await page.mouse.up();

  await page.waitForTimeout(200);
  expect(await fixture(page, 'calls')).toEqual([]);
});

test('the session\'s own tab is the session: dragging it moves nothing', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await tab(page, 'Source').dragTo(row(page, 'Dest'));
  await page.waitForTimeout(200);
  expect(await fixture(page, 'calls')).toEqual([]);
  expect(await fixture(page, 'selected')).toEqual({ sessionId: 'src', windowIdx: 0 });
});

test('"Move to session…" picks the session in a list that shows the refusals', async ({ page }) => {
  await gotoFixture(page);
  await tab(page, 'worker').click({ button: 'right' });
  await page.locator('[data-menu-action="move-to-session"]').click();

  const dialog = page.locator('.move-tab-dialog');
  await expect(dialog.locator('h2')).toHaveText('Move “worker” to session');
  await expect(dialog.locator('.move-target')).toHaveText([/Dest/, /Other/, /Remote/]);
  const remote = dialog.locator('.move-target', { hasText: 'Remote' });
  await expect(remote).toHaveAttribute('aria-disabled', 'true');
  await expect(remote).toContainText('cannot join a session on a server');

  // A refused one cannot be chosen.
  await remote.click({ force: true });
  await expect(dialog.locator('.btn-primary')).toBeDisabled();

  await dialog.locator('.move-target', { hasText: 'Other' }).click();
  await dialog.locator('.btn-primary').click();
  await expect(dialog).toHaveCount(0);
  expect(await fixture(page, 'calls')).toEqual([
    { method: 'MoveTabToSession', args: ['src', 1, 'other', 'project-a'] },
  ]);
});

test('the picker is driven from the keyboard: filter, Enter', async ({ page }) => {
  await gotoFixture(page);
  await tab(page, 'worker').click({ button: 'right' });
  await page.locator('[data-menu-action="move-to-session"]').click();
  // The filter takes the keyboard as the picker opens.
  await expect(page.locator('.move-tab-dialog .move-filter')).toBeFocused();
  await page.keyboard.type('oth');
  await page.keyboard.press('Enter');
  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MoveTabToSession', args: ['src', 1, 'other', 'project-a'] },
  ]);
});

test('"Move to new session" makes the tab a session and opens it', async ({ page }) => {
  await gotoFixture(page);
  await tab(page, 'worker').click({ button: 'right' });
  await page.locator('[data-menu-action="move-to-new-session"]').click();
  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MoveTabToNewSession', args: ['src', 1, '', 'project-a'] },
  ]);
  await expect.poll(() => fixture(page, 'selected')).toEqual({ sessionId: 'split', windowIdx: 0 });
  await expect.poll(() => fixture(page, 'notices')).toEqual(['“worker” is now a session of its own: worker']);
});

test('the session\'s own tab offers no move', async ({ page }) => {
  await gotoFixture(page);
  await tab(page, 'Source').click({ button: 'right' });
  await expect(page.locator('.tab-context-menu')).toBeVisible();
  await expect(page.locator('[data-menu-action="move-to-session"]')).toHaveCount(0);
  await expect(page.locator('[data-menu-action="move-to-new-session"]')).toHaveCount(0);
});

test('"Merge into session…" folds the whole session into the chosen one', async ({ page }) => {
  await gotoFixture(page);
  await row(page, 'Source').click({ button: 'right' });
  await page.locator('[data-menu-action="merge-into"]').click();

  const dialog = page.locator('.move-tab-dialog');
  await expect(dialog.locator('h2')).toHaveText('Merge “Source” into session');
  await expect(dialog.locator('.move-note')).toContainText('“Source” is then removed');
  await dialog.locator('.move-target', { hasText: 'Dest' }).dblclick();

  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MergeSessionInto', args: ['src', 'dst', 'project-a'] },
  ]);
  await expect(dialog).toHaveCount(0);
  await expect(row(page, 'Source')).toHaveCount(0);
  await expect.poll(() => fixture(page, 'selected')).toEqual({ sessionId: 'dst', windowIdx: 7 });
  await expect.poll(() => fixture(page, 'notices')).toEqual(['“Source” merged into Dest (2 tabs)']);
});
