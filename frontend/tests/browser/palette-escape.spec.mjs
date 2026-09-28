import { test, expect } from '@playwright/test';

/**
 * Escape closes the command palette wherever the focus is, and goes no
 * further. Its key handler sat on the palette, so after a click beside the
 * search field the focus was on the page and Escape did nothing.
 */
async function openPalette(page) {
  await page.goto('/tests/browser/dialog-races-fixture.html?mode=palette');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await expect(page.locator('.command-palette')).toBeVisible();
  // Something behind the palette that would react to Escape.
  await page.evaluate(() => {
    window.escapesBehind = 0;
    window.addEventListener('keydown', (e) => { if (e.key === 'Escape') window.escapesBehind++; });
    document.body.addEventListener('keydown', (e) => { if (e.key === 'Escape') window.escapesBehind++; });
  });
}

for (const [where, click] of [
  ['beside the palette', (page) => page.mouse.click(5, 5)],
  ['on the palette outside the search field', (page) => page.locator('.palette-results').click({ position: { x: 2, y: 2 } })],
]) {
  test(`Escape closes the palette after a click ${where}`, async ({ page }) => {
    await openPalette(page);
    await click(page);
    await expect(page.locator('.command-palette input')).not.toBeFocused();
    await page.keyboard.press('Escape');
    await expect(page.locator('.command-palette')).toHaveCount(0);
    expect(await page.evaluate(() => window.escapesBehind)).toBe(0);
  });
}

test('Escape in the search field still closes it, and goes no further', async ({ page }) => {
  await openPalette(page);
  await page.locator('.command-palette input').focus();
  await page.keyboard.press('Escape');
  await expect(page.locator('.command-palette')).toHaveCount(0);
  expect(await page.evaluate(() => window.escapesBehind)).toBe(0);
});
