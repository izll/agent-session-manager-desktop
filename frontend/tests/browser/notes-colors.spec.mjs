import { test, expect } from '@playwright/test';

// The notes' background and text colours, chosen in Settings → General →
// Appearance. Runs the real Settings dialog beside the real notes view
// (notes-colors-fixture.ts): a pick in the one has to show in the other at
// once, with no reload.
const WEBKIT = { tag: '@webkit' };

async function open(page) {
  await page.goto('/tests/browser/notes-colors-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await expect(page.locator('.notes-textarea')).toHaveValue(/Shopping list/);
}

const bgSwatch = (page, id) =>
  page.locator(`[data-testid="notes-background-swatches"] [data-notes-color="${id}"]`);
const textSwatch = (page, id) =>
  page.locator(`[data-testid="notes-text-swatches"] [data-notes-color="${id}"]`);
const lastSave = (page) => page.evaluate(() => window.notesColorsFixture.lastSave());

/** "rgb(r, g, b)" → relative luminance, to tell dark text from light. */
function luminance(css) {
  const [r, g, b] = css.match(/\d+(\.\d+)?/g).slice(0, 3).map(Number).map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
const textColor = (page) =>
  page.locator('.notes-textarea').evaluate((el) => getComputedStyle(el).color);

// The look before the setting existed: a dark wash and white text.
const DEFAULT_BG = 'rgba(0, 0, 0, 0.2)';
const DEFAULT_TEXT = 'rgb(255, 255, 255)';

test('a preset background recolours the notes, Auto text follows it, Default restores', WEBKIT, async ({ page }) => {
  await open(page);
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveCSS('background-color', DEFAULT_BG);
  await expect(textarea).toHaveCSS('color', DEFAULT_TEXT);
  await expect(bgSwatch(page, 'default')).toHaveClass(/selected/);
  await expect(textSwatch(page, 'auto')).toHaveClass(/selected/);

  // Paper: a warm light page, and Auto turns the text dark on it.
  await bgSwatch(page, 'paper').click();
  await expect(textarea).toHaveCSS('background-color', 'rgb(244, 236, 216)');
  await expect.poll(async () => luminance(await textColor(page))).toBeLessThan(0.1);
  // The caret is drawn in the text colour, not left white on the pale page.
  const caret = await textarea.evaluate((el) => getComputedStyle(el).caretColor);
  expect(caret).toBe(await textColor(page));
  await expect(bgSwatch(page, 'paper')).toHaveClass(/selected/);
  await expect.poll(async () => (await lastSave(page))?.notesBackground).toBe('paper');

  // Dim is dark: Auto turns the text light again.
  await bgSwatch(page, 'dim').click();
  await expect(textarea).toHaveCSS('background-color', 'rgb(42, 44, 49)');
  await expect.poll(async () => luminance(await textColor(page))).toBeGreaterThan(0.6);

  // Default puts back exactly what was there before.
  await bgSwatch(page, 'default').click();
  await expect(textarea).toHaveCSS('background-color', DEFAULT_BG);
  await expect(textarea).toHaveCSS('color', DEFAULT_TEXT);
  await expect.poll(async () => (await lastSave(page))?.notesBackground).toBe('');
});

test('a chosen text colour is used over Auto, and a poor pairing is pointed out', WEBKIT, async ({ page }) => {
  await open(page);
  const textarea = page.locator('.notes-textarea');
  const warning = page.getByTestId('notes-contrast-warning');

  await bgSwatch(page, 'paper').click();
  await textSwatch(page, 'sepia').click();
  await expect(textarea).toHaveCSS('color', 'rgb(91, 70, 54)');
  await expect(textarea).toHaveCSS('background-color', 'rgb(244, 236, 216)');
  await expect(warning).toHaveCount(0);
  await expect.poll(async () => (await lastSave(page))?.notesText).toBe('sepia');

  // White on paper can be chosen, but not without a word.
  await textSwatch(page, 'white').click();
  await expect(textarea).toHaveCSS('color', 'rgb(244, 244, 245)');
  await expect(warning).toBeVisible();

  // A text colour on the default background leaves the background alone.
  await bgSwatch(page, 'default').click();
  await expect(textarea).toHaveCSS('background-color', DEFAULT_BG);
  await expect(textarea).toHaveCSS('color', 'rgb(244, 244, 245)');
  await expect(warning).toHaveCount(0);

  // Back to Auto: the text follows the background again.
  await textSwatch(page, 'auto').click();
  await expect(textarea).toHaveCSS('color', DEFAULT_TEXT);
  await bgSwatch(page, 'light').click();
  await expect.poll(async () => luminance(await textColor(page))).toBeLessThan(0.1);
  await expect.poll(async () => (await lastSave(page))?.notesText).toBe('');
});

test('custom colours are picked with the colour picker', async ({ page }) => {
  await open(page);
  const textarea = page.locator('.notes-textarea');

  await bgSwatch(page, 'custom').locator('input[type="color"]').fill('#1e3a8a');
  await expect(textarea).toHaveCSS('background-color', 'rgb(30, 58, 138)');
  await expect(bgSwatch(page, 'custom')).toHaveClass(/selected/);
  await expect.poll(async () => luminance(await textColor(page))).toBeGreaterThan(0.6);
  await expect.poll(async () => {
    const saved = await lastSave(page);
    return [saved?.notesBackground, saved?.notesBackgroundColor];
  }).toEqual(['custom', '#1e3a8a']);

  await textSwatch(page, 'custom').locator('input[type="color"]').fill('#fde68a');
  await expect(textarea).toHaveCSS('color', 'rgb(253, 230, 138)');
  await expect(textSwatch(page, 'custom')).toHaveClass(/selected/);
  await expect.poll(async () => {
    const saved = await lastSave(page);
    return [saved?.notesText, saved?.notesTextColor];
  }).toEqual(['custom', '#fde68a']);

  await bgSwatch(page, 'default').click();
  await textSwatch(page, 'auto').click();
  await expect(textarea).toHaveCSS('background-color', DEFAULT_BG);
  await expect(textarea).toHaveCSS('color', DEFAULT_TEXT);
});
