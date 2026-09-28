import { test, expect } from '@playwright/test';

async function gotoFixture(page) {
  await page.goto('/tests/browser/sidebar-favorites-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

const names = (page) => page.locator('.session-list .session-name');
const header = (page) => page.locator('.section-header.favorites');
// The row the keyboard is on: selected, and not the fainter second copy.
const cursorRow = (page) => page.locator('.session-list .session-item.selected:not(.echo) .session-name');
const step = (page, dir) => page.evaluate((d) => window.sidebarFavoritesFixture[d](), dir);

test('the favourites header folds and unfolds the section, and it is saved', async ({ page }) => {
  await gotoFixture(page);
  await expect(names(page)).toHaveText(['Loose fav', 'Grouped fav', 'Grouped fav', 'Grouped plain', 'Plain one']);
  await expect(header(page)).toHaveAttribute('aria-expanded', 'true');
  await expect(header(page)).toHaveAttribute('title', 'Collapse favorites');
  await expect(header(page).locator('.favorites-count')).toHaveText('2');

  await header(page).click();
  // The copies go; the sessions stay — the loose favourite among the ungrouped.
  await expect(names(page)).toHaveText(['Grouped fav', 'Grouped plain', 'Loose fav', 'Plain one']);
  await expect(header(page)).toHaveAttribute('aria-expanded', 'false');
  await expect(header(page)).toHaveAttribute('title', 'Expand favorites');
  await expect(header(page).locator('.favorites-count')).toHaveText('2');
  let saved = await page.evaluate(() => window.sidebarFavoritesFixture.saved());
  expect(saved.at(-1)).toMatchObject({ favoritesCollapsed: true });

  // From the keyboard too: it is a button.
  await header(page).focus();
  await page.keyboard.press('Enter');
  await expect(names(page)).toHaveText(['Loose fav', 'Grouped fav', 'Grouped fav', 'Grouped plain', 'Plain one']);
  saved = await page.evaluate(() => window.sidebarFavoritesFixture.saved());
  expect(saved.at(-1)).toMatchObject({ favoritesCollapsed: false });
});

test('stepping skips a folded section and goes on from the other copy', async ({ page }) => {
  await gotoFixture(page);
  await step(page, 'next');
  await expect(cursorRow(page)).toHaveText('Loose fav');
  await step(page, 'next');
  await expect(cursorRow(page)).toHaveText('Grouped fav');

  // The selected copy is folded away: the one in the group stands for it.
  await header(page).click();
  await expect(cursorRow(page)).toHaveText('Grouped fav');
  await step(page, 'next');
  await expect(cursorRow(page)).toHaveText('Grouped plain');
  await step(page, 'next');
  await expect(cursorRow(page)).toHaveText('Loose fav');
  await step(page, 'next');
  await expect(cursorRow(page)).toHaveText('Plain one');
  // Back up to the top: nothing from the folded section on the way.
  await step(page, 'prev');
  await step(page, 'prev');
  await step(page, 'prev');
  await expect(cursorRow(page)).toHaveText('Grouped fav');
  await step(page, 'prev');
  await expect(cursorRow(page)).toHaveText('Grouped fav');
});

test('a search keeps the section folded and counts what matches', async ({ page }) => {
  await gotoFixture(page);
  await header(page).click();
  await page.locator('.search-input').fill('grouped');
  await expect(header(page)).toHaveAttribute('aria-expanded', 'false');
  await expect(header(page).locator('.favorites-count')).toHaveText('1');
  await expect(names(page)).toHaveText(['Grouped fav', 'Grouped plain']);
  // Nothing matching among the favourites: no header at all, as before.
  await page.locator('.search-input').fill('plain');
  await expect(header(page)).toHaveCount(0);
});

// Folding the favourites moved the cursor from the favourite's copy to its copy
// in a group far below, and the list scrolled down there although the user
// had not moved. Only a step or a selection scrolls.
test('folding the favourites does not scroll to the other copy', async ({ page }) => {
  await page.goto('/tests/browser/sidebar-favorites-fixture.html?many=1');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  const list = page.locator('.session-list');
  const scrollTop = () => list.evaluate((el) => el.scrollTop);

  await page.locator('.session-list .session-name', { hasText: 'Grouped fav' }).first().click();
  await list.evaluate((el) => { el.scrollTop = 0; });
  await header(page).click();
  await page.waitForTimeout(150);
  expect(await scrollTop()).toBe(0);

  // A step still brings the cursor into view: from the grouped copy, below
  // the forty fillers, to the next one in the group.
  await step(page, 'next');
  await expect(cursorRow(page)).toHaveText('Grouped plain');
  await expect(page.locator('.session-list .session-item.selected:not(.echo)')).toBeInViewport();
});
