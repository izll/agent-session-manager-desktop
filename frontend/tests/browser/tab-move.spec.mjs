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
  await expect(dialog.locator('.move-target')).toHaveText([/Dest/, /Other/, /Remote/, /Solo/]);
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

test('"Move to new session" asks the name, offered the tab\'s, and opens the new session', async ({ page }) => {
  await gotoFixture(page);
  await tab(page, 'worker').click({ button: 'right' });
  await page.locator('[data-menu-action="move-to-new-session"]').click();

  // Asked first: Escape leaves everything as it was.
  const prompt = page.locator('.move-new-session-dialog');
  await expect(prompt.locator('h2')).toHaveText('Move “worker” to a new session');
  await expect(prompt.locator('input')).toHaveValue('worker');
  await expect(prompt.locator('input')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(prompt).toHaveCount(0);
  await page.waitForTimeout(200);
  expect(await fixture(page, 'calls')).toEqual([]);

  // Enter takes the name offered.
  await tab(page, 'worker').click({ button: 'right' });
  await page.locator('[data-menu-action="move-to-new-session"]').click();
  await expect(prompt.locator('input')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MoveTabToNewSession', args: ['src', 1, 'worker', 'project-a'] },
  ]);
  await expect(prompt).toHaveCount(0);
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

// ── A new session by drag, and the only tab of a session ──────────────────

const newSessionZone = (page) => page.locator('[data-drop-target="new-session"]');

/**
 * Starts dragging source, then holds it over a target that only exists while
 * a tab is dragged — Playwright's dragTo would wait for it before moving.
 */
async function holdOverAppearing(page, source, target) {
  await source.hover();
  await page.mouse.down();
  const from = await source.boundingBox();
  await page.mouse.move(from.x + from.width / 2 + 8, from.y + from.height / 2 + 8, { steps: 4 });
  await expect(target).toBeVisible();
  const box = await target.boundingBox();
  await page.mouse.move(box.x + 10, box.y + box.height / 2, { steps: 4 });
  await page.mouse.move(box.x + 20, box.y + box.height / 2, { steps: 4 });
}

async function selectSolo(page) {
  await page.evaluate(() => window.tabMoveFixture.select('solo'));
  await expect(tab(page, 'Solo')).toBeVisible();
}

test('a tab dropped on "New session" becomes a session of the name typed', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await expect(newSessionZone(page)).toHaveCount(0);
  await holdOverAppearing(page, tab(page, 'worker'), newSessionZone(page));
  await expect(newSessionZone(page)).toHaveClass(/tab-drop-ok/);
  await page.mouse.up();
  await expect(newSessionZone(page)).toHaveCount(0);

  const prompt = page.locator('.move-new-session-dialog');
  await expect(prompt.locator('input')).toHaveValue('worker');
  await prompt.locator('input').fill('Builds');
  await prompt.locator('input').press('Enter');
  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MoveTabToNewSession', args: ['src', 1, 'Builds', 'project-a'] },
  ]);
  await expect(prompt).toHaveCount(0);
  await expect(row(page, 'Builds')).toHaveCount(1);
  await expect.poll(() => fixture(page, 'selected')).toEqual({ sessionId: 'split', windowIdx: 0 });
  await expect.poll(() => fixture(page, 'notices')).toEqual(['“worker” is now a session of its own: Builds']);
});

test('the "New session" target is there only while a tab is dragged', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await holdOverAppearing(page, tab(page, 'worker'), row(page, 'Other'));
  await expect(newSessionZone(page)).toBeVisible();
  await expect(newSessionZone(page)).not.toHaveClass(/tab-drop-ok/);
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await expect(newSessionZone(page)).toHaveCount(0);
  await page.waitForTimeout(200);
  expect(await fixture(page, 'calls')).toEqual([]);
});

test('the only tab of a session lights up the sessions it can join', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await selectSolo(page);
  await holdOver(page, tab(page, 'Solo'), row(page, 'Other'));
  await expect(row(page, 'Other')).toHaveClass(/tab-drop-ok/);
  // Let go of as a hand would that changed its mind: nothing drops.
  await page.keyboard.press('Escape');
  await page.mouse.up();

  await holdOver(page, tab(page, 'Solo'), row(page, 'Remote'));
  await expect(row(page, 'Remote')).toHaveClass(/tab-drop-refused/);
  await page.mouse.up();

  // It is a session of its own already: no new one to make of it.
  await holdOverAppearing(page, tab(page, 'Solo'), newSessionZone(page));
  await expect(newSessionZone(page)).toHaveClass(/tab-drop-refused/);
  await expect(newSessionZone(page)).toHaveAttribute('title', /only tab/);
  await page.mouse.up();

  await page.waitForTimeout(200);
  expect(await fixture(page, 'calls')).toEqual([]);
  await expect(page.locator('.move-new-session-dialog')).toHaveCount(0);
});

test('the only tab of a session moves once it is confirmed that the session ends', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await selectSolo(page);
  await tab(page, 'Solo').dragTo(row(page, 'Dest'));

  const confirm = page.locator('.dialog-overlay', { hasText: 'has only this tab' });
  await expect(confirm).toContainText('The session “Solo” has only this tab — moving it ends the session. Move it?');
  await page.waitForTimeout(200);
  expect(await fixture(page, 'calls')).toEqual([]);
  await confirm.getByRole('button', { name: 'Move' }).click();

  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MergeSessionInto', args: ['solo', 'dst', 'project-a'] },
  ]);
  await expect(row(page, 'Solo')).toHaveCount(0);
  await expect.poll(() => fixture(page, 'selected')).toEqual({ sessionId: 'dst', windowIdx: 7 });
  await expect.poll(() => fixture(page, 'notices')).toEqual(['“Solo” moved to Dest']);
});

test('cancelling the move of the only tab leaves everything as it was', WEBKIT, async ({ page }) => {
  await gotoFixture(page);
  await selectSolo(page);
  await tab(page, 'Solo').dragTo(row(page, 'Dest'));
  const confirm = page.locator('.dialog-overlay', { hasText: 'has only this tab' });
  await confirm.getByRole('button', { name: 'Cancel' }).click();
  await expect(confirm).toHaveCount(0);
  await page.waitForTimeout(200);
  expect(await fixture(page, 'calls')).toEqual([]);
  await expect(row(page, 'Solo')).toHaveCount(1);
  expect(await fixture(page, 'selected')).toEqual({ sessionId: 'solo', windowIdx: 0 });
});

test('the only tab\'s menu moves it to a session, asking first, and offers no new one', async ({ page }) => {
  await gotoFixture(page);
  await selectSolo(page);
  await tab(page, 'Solo').click({ button: 'right' });
  const toNew = page.locator('[data-menu-action="move-to-new-session"]');
  await expect(toNew).toHaveAttribute('aria-disabled', 'true');
  await expect(toNew).toHaveAttribute('title', /only tab/);
  await toNew.click({ force: true });
  await expect(page.locator('.move-new-session-dialog')).toHaveCount(0);
  await expect(page.locator('.tab-context-menu')).toBeVisible();

  await page.locator('[data-menu-action="move-to-session"]').click();
  const dialog = page.locator('.move-tab-dialog');
  await expect(dialog.locator('.move-target', { hasText: 'Remote' })).toHaveAttribute('aria-disabled', 'true');
  await dialog.locator('.move-target', { hasText: 'Dest' }).dblclick();
  await expect(dialog).toHaveCount(0);

  const confirm = page.locator('.dialog-overlay', { hasText: 'has only this tab' });
  await expect(confirm).toBeVisible();
  expect(await fixture(page, 'calls')).toEqual([]);
  await confirm.getByRole('button', { name: 'Move' }).click();
  await expect.poll(() => fixture(page, 'calls')).toEqual([
    { method: 'MergeSessionInto', args: ['solo', 'dst', 'project-a'] },
  ]);
});

// Ctrl+PgUp / Ctrl+PgDn are shared: in a note of several pages they step its
// pages, anywhere else — a note of one page included — the session's tabs.
test('in a note of several pages the tab keys step its pages', async ({ page }) => {
  await gotoFixture(page);
  const note = page.locator('.notes-textarea');
  await expect(note).toHaveValue('first page');
  await note.focus();
  await page.keyboard.press('Control+PageDown');
  await expect(note).toHaveValue('second page');
  expect((await page.evaluate(() => window.tabMoveFixture.selected())).windowIdx).toBe(0);
});

test('in a note of one page the tab keys still switch tabs', async ({ page }) => {
  await gotoFixture(page);
  const note = page.locator('.notes-textarea');
  await expect(note).toHaveValue('first page');
  // Out of the note, they switch tabs.
  await page.locator('body').click({ position: { x: 600, y: 500 } });
  await page.keyboard.press('Control+PageDown');
  await expect.poll(async () => (await page.evaluate(() => window.tabMoveFixture.selected())).windowIdx).not.toBe(0);
  // In a note of one page too.
  await expect(note).toHaveValue(/^note of src:/);
  const before = (await page.evaluate(() => window.tabMoveFixture.selected())).windowIdx;
  await note.focus();
  await page.keyboard.press('Control+PageDown');
  await expect.poll(async () => (await page.evaluate(() => window.tabMoveFixture.selected())).windowIdx).not.toBe(before);
});
