import { test, expect } from '@playwright/test';

async function gotoFixture(page) {
  await page.goto('/tests/browser/sidebar-filter-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

const names = (page) => page.locator('.session-list .session-name');
const filterButton = (page) => page.locator('.filter-toggle');
const menu = (page) => page.locator('.filter-menu');

// The reported bug: with the list sorted, typing in the search changed nothing.
test('the search filters the list while it is sorted by activity', async ({ page }) => {
  await gotoFixture(page);
  await page.locator('.sort-toggle:not(.filter-toggle)').click();
  await expect(names(page)).toHaveCount(4);
  await page.locator('.search-input').fill('api');
  await expect(names(page)).toHaveText(['Running api', 'Weekly api']);
  await page.locator('.search-input').fill('nothing like this');
  await expect(names(page)).toHaveCount(0);
  await expect(page.locator('.no-matches')).toContainText('No matching sessions');
});

test('the filter menu hides inactive and old sessions, together', async ({ page }) => {
  await gotoFixture(page);
  await expect(filterButton(page)).toHaveAttribute('title', 'Filter sessions');
  await expect(filterButton(page)).not.toHaveClass(/active/);

  await filterButton(page).click();
  await expect(menu(page)).toBeVisible();
  await menu(page).getByRole('menuitemcheckbox', { name: 'Hide inactive' }).click();
  // The menu stays open, so the time window can be set too.
  await expect(menu(page)).toBeVisible();
  await expect(names(page)).toHaveText(['Running api', 'Idle runner']);
  // The empty group goes with its sessions.
  await expect(page.getByText('Old group')).toHaveCount(0);

  await menu(page).getByRole('menuitemradio', { name: 'Active in the last 7 days' }).click();
  await expect(names(page)).toHaveText(['Running api']);
  await expect(menu(page).getByRole('menuitemradio', { name: 'Active in the last 7 days' }))
    .toHaveAttribute('aria-checked', 'true');
  await expect(menu(page).getByRole('menuitemradio', { name: 'Any time' }))
    .toHaveAttribute('aria-checked', 'false');

  await expect(filterButton(page)).toHaveClass(/active/);
  await expect(filterButton(page))
    .toHaveAttribute('title', 'Filter: Hide inactive · Active in the last 7 days');
  await expect(page.locator('.filter-dot')).toBeVisible();

  // Both halves were saved.
  const saved = await page.evaluate(() => window.sidebarFilterFixture.saved());
  expect(saved.at(-1)).toMatchObject({ sidebarHideInactive: true, sidebarActiveWithinDays: 7 });

  // A click elsewhere closes the menu.
  await page.locator('.status-summary').click();
  await expect(menu(page)).toHaveCount(0);

  // And it applies in the activity order too.
  await page.locator('.sort-toggle:not(.filter-toggle)').click();
  await expect(names(page)).toHaveText(['Running api']);
});

test('an emptied list says so and clears the filter', async ({ page }) => {
  await gotoFixture(page);
  await filterButton(page).click();
  await menu(page).getByRole('menuitemradio', { name: 'Active in the last day' }).click();
  await page.keyboard.press('Escape');
  await expect(menu(page)).toHaveCount(0);
  await page.locator('.search-input').fill('weekly');
  await expect(names(page)).toHaveCount(0);
  await expect(page.locator('.no-matches')).toContainText('No sessions match the filter');
  await page.locator('.no-matches').getByRole('button', { name: 'Clear filter' }).click();
  await expect(names(page)).toHaveCount(4);
  await expect(page.locator('.search-input')).toHaveValue('');
  await expect(filterButton(page)).not.toHaveClass(/active/);
});
