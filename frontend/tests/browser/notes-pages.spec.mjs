import { test, expect } from '@playwright/test';

// A note is a list of titled pages. These run the real Notes view against a
// stand-in backend (notes-fixture.ts) that keeps each note's pages.
//
// Run on WebKit as well as Chromium (see playwright.config.mjs): the app's
// webview is WebKit on Linux and macOS, and the page strip leans on what
// engines differ in — HTML5 drag and drop, and keys with Alt.
const WEBKIT = { tag: '@webkit' };

async function gotoNotes(page, sessionId) {
  await page.goto('/tests/browser/notes-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  // The fixture opens on notes-a, whose first save is refused on purpose.
  await page.evaluate((id) => window.notesFixture.select(id), sessionId);
}

const pageTab = (page, name) => page.locator('.page-tab', { hasText: name });
const storedPages = (page, id) => page.evaluate((sid) => window.notesFixture.storedPages(sid), id);

test('a text-only note opens as one untitled page', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-b');
  await expect(page.locator('.notes-textarea')).toHaveValue('saved B');
  await expect(page.locator('.page-tab')).toHaveCount(1);
  await expect(page.locator('.page-tab')).toHaveText('Note');
  await expect(page.locator('.page-tab')).toHaveAttribute('aria-selected', 'true');
});

test('a new page is added with a title and saved with the rest of the note', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-b');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('saved B');

  await page.getByRole('button', { name: 'New page' }).click();
  const title = page.locator('.page-title-input');
  await expect(title).toBeFocused();
  await title.fill('Ideas');
  await title.press('Enter');
  await expect(textarea).toBeFocused();
  await expect(textarea).toHaveValue('');
  await expect(pageTab(page, 'Ideas')).toHaveAttribute('aria-selected', 'true');
  // With two pages the untitled one is told apart by its place.
  await expect(page.locator('.page-tab').first()).toHaveText('Note 1');

  await textarea.fill('a fresh idea');
  await expect.poll(() => storedPages(page, 'notes-b'))
    .toEqual([['', 'saved B'], ['Ideas', 'a fresh idea']]);
  await expect(page.locator('.save-indicator.unsaved')).toHaveCount(0);
});

test('switching pages shows each page and loses no edit', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('alpha and beta');
  await expect(pageTab(page, 'Plan')).toHaveAttribute('aria-selected', 'true');

  // Typed and switched away at once, inside the autosave delay: the edit on
  // the page left must still be saved, together with the one on the next.
  await textarea.fill('alpha, beta and gamma');
  await pageTab(page, 'Risks').click();
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
  await textarea.fill('no risks left');
  await expect.poll(() => storedPages(page, 'notes-pages'))
    .toEqual([['Plan', 'alpha, beta and gamma'], ['Risks', 'no risks left']]);

  // Alt+PgUp / Alt+PgDn step through the pages from the editor.
  await textarea.press('Alt+PageUp');
  await expect(textarea).toHaveValue('alpha, beta and gamma');
  await textarea.press('Alt+PageDown');
  await expect(textarea).toHaveValue('no risks left');

  // Another session and back: the note opens on the page it was left on.
  await page.evaluate(() => window.notesFixture.select('notes-b'));
  await expect(textarea).toHaveValue('saved B');
  await page.evaluate(() => window.notesFixture.select('notes-pages'));
  await expect(textarea).toHaveValue('no risks left');
  await expect(pageTab(page, 'Risks')).toHaveAttribute('aria-selected', 'true');
});

// The page keys are shortcuts like any other: rebound in the settings, the
// new keys step and the old ones do nothing, and the tooltip names the new.
test('the page keys follow a rebinding', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('alpha and beta');
  await expect(pageTab(page, 'Plan')).toHaveAttribute('title', /Alt\+PgUp \/ Alt\+PgDn switch pages\.$/);

  await page.evaluate(() => window.notesFixture.rebind({
    'notes.nextPage': [{ key: 'arrowright', alt: true, shift: true }],
    'notes.prevPage': [],
  }));
  await expect(pageTab(page, 'Plan')).toHaveAttribute('title', /Shift\+Alt\+→ switch pages\.$/);
  await textarea.press('Alt+PageDown');
  await expect(textarea).toHaveValue('alpha and beta');
  await textarea.press('Alt+Shift+ArrowRight');
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
  // Switched off: Alt+PgUp steps no more.
  await textarea.press('Alt+PageUp');
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
});

// Each page keeps its undo history across page switches; the histories go
// with the note, so another note and back starts them over.
test('undo reaches edits made before switching pages', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('alpha and beta');
  const undo = () => textarea.press('ControlOrMeta+z');
  const redo = () => textarea.press('ControlOrMeta+y');

  await textarea.fill('plan edited');
  await pageTab(page, 'Risks').click();
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
  await textarea.fill('risks edited');
  await pageTab(page, 'Plan').click();
  await expect(textarea).toHaveValue('plan edited');

  await undo();
  await expect(textarea).toHaveValue('alpha and beta');
  // Undone text is saved like typed text.
  await expect.poll(() => storedPages(page, 'notes-pages'))
    .toEqual([['Plan', 'alpha and beta'], ['Risks', 'risks edited']]);
  await redo();
  await expect(textarea).toHaveValue('plan edited');

  // The other page's history went with it, and its undo stays on that page.
  await textarea.press('Alt+PageDown');
  await expect(textarea).toHaveValue('risks edited');
  await undo();
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
  await undo();
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
  await expect.poll(() => storedPages(page, 'notes-pages'))
    .toEqual([['Plan', 'plan edited'], ['Risks', 'first line\nthe hidden needle is here']]);
  await expect(page.locator('.save-indicator.unsaved')).toHaveCount(0);

  // Another note and back: the histories are not kept for notes left.
  await page.evaluate(() => window.notesFixture.select('notes-b'));
  await expect(textarea).toHaveValue('saved B');
  await page.evaluate(() => window.notesFixture.select('notes-pages'));
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
  await pageTab(page, 'Plan').click();
  await expect(textarea).toHaveValue('plan edited');
  await undo();
  await expect(textarea).toHaveValue('plan edited');
});

test('the open page is remembered across a restart', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  await pageTab(page, 'Risks').click();
  await expect(page.locator('.notes-textarea')).toHaveValue(/hidden needle/);

  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await page.evaluate(() => window.notesFixture.select('notes-pages'));
  await expect(page.locator('.notes-textarea')).toHaveValue(/hidden needle/);
  await expect(pageTab(page, 'Risks')).toHaveAttribute('aria-selected', 'true');
});

test('a page is renamed by double-click and from its menu', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  await expect(page.locator('.notes-textarea')).toHaveValue('alpha and beta');

  await pageTab(page, 'Plan').dblclick();
  const title = page.locator('.page-title-input');
  await expect(title).toBeFocused();
  await expect(title).toHaveValue('Plan');
  await title.fill('Roadmap');
  await title.press('Enter');
  await expect(pageTab(page, 'Roadmap')).toBeVisible();

  await pageTab(page, 'Risks').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename' }).click();
  await expect(title).toBeFocused();
  await title.fill('Dangers');
  // Escape keeps the old title.
  await title.press('Escape');
  await expect(pageTab(page, 'Risks')).toBeVisible();

  await expect.poll(() => storedPages(page, 'notes-pages'))
    .toEqual([['Roadmap', 'alpha and beta'], ['Risks', 'first line\nthe hidden needle is here']]);
});

test('deleting a page with text asks first; an empty page goes at once', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('alpha and beta');

  await pageTab(page, 'Risks').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete page' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Risks');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.page-tab')).toHaveCount(2);

  await pageTab(page, 'Risks').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete page' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete page' }).click();
  await expect(page.locator('.page-tab')).toHaveCount(1);
  await expect.poll(() => storedPages(page, 'notes-pages')).toEqual([['Plan', 'alpha and beta']]);

  // An empty page: no question.
  await page.getByRole('button', { name: 'New page' }).click();
  await page.locator('.page-title-input').press('Enter');
  await expect(page.locator('.page-tab')).toHaveCount(2);
  await page.locator('.page-tab').nth(1).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete page' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('.page-tab')).toHaveCount(1);
  await expect(textarea).toHaveValue('alpha and beta');

  // The last page cannot be deleted.
  await pageTab(page, 'Plan').click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Delete page' })).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(page.locator('.page-menu')).toHaveCount(0);
});

test('pages are reordered from the menu and by dragging', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  await expect(page.locator('.notes-textarea')).toHaveValue('alpha and beta');
  const order = () => page.locator('.page-tab').allTextContents();

  await pageTab(page, 'Plan').click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Move left' })).toBeDisabled();
  await page.getByRole('menuitem', { name: 'Move right' }).click();
  await expect.poll(order).toEqual(['Risks', 'Plan']);
  await expect.poll(() => storedPages(page, 'notes-pages').then((p) => p?.map(([title]) => title)))
    .toEqual(['Risks', 'Plan']);

  await pageTab(page, 'Plan').dragTo(pageTab(page, 'Risks'), { targetPosition: { x: 2, y: 5 } });
  await expect.poll(order).toEqual(['Plan', 'Risks']);
  await expect.poll(() => storedPages(page, 'notes-pages').then((p) => p?.map(([title]) => title)))
    .toEqual(['Plan', 'Risks']);
  // The open page is still the one that was open.
  await expect(page.locator('.notes-textarea')).toHaveValue('alpha and beta');
});

// A page tab dragged out of the strip and dropped into the note is not text:
// nothing is typed, and the pages stay as they were.
test('a page dropped onto the note text inserts nothing', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('alpha and beta');
  const order = () => page.locator('.page-tab').allTextContents();

  await pageTab(page, 'Risks').dragTo(textarea, { targetPosition: { x: 40, y: 10 } });
  // The marker is cleared and the strip is usable again: a drag that ended
  // anywhere leaves nothing behind.
  await expect(page.locator('.page-tab.drop-before, .page-tab.drop-after')).toHaveCount(0);
  await expect(textarea).toHaveValue('alpha and beta');
  await expect.poll(order).toEqual(['Plan', 'Risks']);
  await expect(pageTab(page, 'Plan')).toHaveAttribute('aria-selected', 'true');
  await page.waitForTimeout(700);
  expect(await storedPages(page, 'notes-pages')).toEqual([
    ['Plan', 'alpha and beta'], ['Risks', 'first line\nthe hidden needle is here'],
  ]);

  // And dragging along the strip still works afterwards.
  await pageTab(page, 'Risks').dragTo(pageTab(page, 'Plan'), { targetPosition: { x: 2, y: 5 } });
  await expect.poll(order).toEqual(['Risks', 'Plan']);
});

test('a search result opens its note on the page of the match', WEBKIT, async ({ page }) => {
  await gotoNotes(page, 'notes-pages');
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('alpha and beta');

  await page.evaluate(() => window.notesFixture.jump({
    projectId: 'project-a', sessionId: 'notes-pages', scope: 'tab', query: 'needle', pageId: 'p-risks',
  }));
  await expect(pageTab(page, 'Risks')).toHaveAttribute('aria-selected', 'true');
  await expect(textarea).toHaveValue('first line\nthe hidden needle is here');
  await expect(page.locator('.find-bar input')).toHaveValue('needle');
  await expect.poll(() => textarea.evaluate((el) => el.value.slice(el.selectionStart, el.selectionEnd)))
    .toBe('needle');
});
