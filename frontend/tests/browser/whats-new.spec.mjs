import { test, expect } from '@playwright/test';

// The release notes: opened by hand on the running version, opened after an
// update on everything since the last one, paged with the buttons, the
// version list and the arrow keys, and closed with Escape.

async function open(page, query = '') {
  await page.goto(`/tests/browser/whats-new-fixture.html${query}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

const body = (page) => page.locator('.whats-new .dialog-body');
const versions = (page) => page.locator('.release-version').allTextContents();
const newer = (page) => page.getByRole('button', { name: /Newer/ });
const older = (page) => page.getByRole('button', { name: /Older/ });

test('opened by hand, it shows the running version, one release a page', async ({ page }) => {
  await open(page);
  await expect(page.locator('.dialog-header h2')).toHaveText("What's new");
  await expect(body(page)).toHaveAttribute('data-page', '1.1.17');
  expect(await versions(page)).toEqual(['1.1.17']);
  await expect(page.locator('.since')).toHaveCount(0);
});

test('the buttons page through every release, and stop at the ends', async ({ page }) => {
  await open(page);
  await newer(page).click();
  await expect(body(page)).toHaveAttribute('data-page', '1.1.18');
  await expect(newer(page)).toBeDisabled();
  for (const v of ['1.1.17', '1.1.16', '1.1.15', '1.1.14', '1.1.13']) {
    await older(page).click();
    await expect(body(page)).toHaveAttribute('data-page', v);
  }
  await expect(older(page)).toBeDisabled();
  // The oldest one here has an intro paragraph above its sections.
  await expect(page.locator('.release .intro')).toHaveText('An intro paragraph.');
});

test('the arrow keys page too: → older, ← newer', async ({ page }) => {
  await open(page);
  await page.keyboard.press('ArrowRight');
  await expect(body(page)).toHaveAttribute('data-page', '1.1.16');
  await page.keyboard.press('ArrowRight');
  await expect(body(page)).toHaveAttribute('data-page', '1.1.15');
  await page.keyboard.press('ArrowLeft');
  await expect(body(page)).toHaveAttribute('data-page', '1.1.16');
});

test('the version list jumps straight to a release', async ({ page }) => {
  await open(page);
  await page.locator('.page-select .select-trigger').click();
  await page.locator('.select-dropdown .select-option', { hasText: '1.1.13' }).click();
  await expect(body(page)).toHaveAttribute('data-page', '1.1.13');
  await expect(page.locator('.page-select .select-value')).toHaveText('1.1.13 · 2026-09-13');
});

test('after an update, the first page holds every release since the last one', async ({ page }) => {
  await open(page, '?launch=1');
  await expect(body(page)).toHaveAttribute('data-page', 'new');
  await expect(page.locator('.since')).toHaveText('Updated from 1.1.14: 4 releases, newest first.');
  expect(await versions(page)).toEqual(['1.1.18', '1.1.17', '1.1.16', '1.1.15']);
  await expect(newer(page)).toBeDisabled();
  // Older goes on to the version updated from.
  await older(page).click();
  await expect(body(page)).toHaveAttribute('data-page', '1.1.14');
  await newer(page).click();
  await expect(body(page)).toHaveAttribute('data-page', 'new');
});

test('the notes render bold, code and links, and their markup stays text', async ({ page }) => {
  await open(page, '?launch=1');
  const first = page.locator('.release').first();
  await expect(first.locator('h4')).toHaveText(['Added', 'Fixed']);
  await expect(first.locator('li strong').first()).toHaveText('The favourites section folds.');
  await expect(first.locator('li code').first()).toHaveText('Enter');

  // The injected markup is on screen as characters; nothing was created or run.
  await expect(first.locator('li').nth(1)).toContainText('<img src=x onerror="window.pwned=1"><script>');
  await expect(page.locator('.whats-new img, .whats-new script')).toHaveCount(0);
  expect(await page.evaluate(() => (window).pwned)).toBeUndefined();

  // A web link opens in the browser; a javascript: one is not a link at all.
  const links = first.locator('li a');
  await expect(links).toHaveCount(1);
  await expect(links).toHaveText('#1');
  await links.click();
  expect(await page.evaluate(() => (window).openedLinks)).toEqual(['https://github.com/izll/agent-session-manager-desktop/pull/1']);
  await expect(first.locator('li').nth(2)).toContainText('not here.');
});

test('Escape closes it and reports the close', async ({ page }) => {
  await open(page);
  await expect(page.locator('.whats-new')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.whats-new')).toHaveCount(0);
  await expect(page.locator('body')).toHaveAttribute('data-closed', '1');
});

test('the close button closes it too', async ({ page }) => {
  await open(page);
  await page.locator('.whats-new .dialog-footer .btn-cancel').click();
  await expect(page.locator('.whats-new')).toHaveCount(0);
  await expect(page.locator('body')).toHaveAttribute('data-closed', '1');
});
