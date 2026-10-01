import { test, expect } from '@playwright/test';

/**
 * Opening the notes hands them the keyboard. The focus stayed in the terminal,
 * so the page keys and undo did nothing until the note was clicked.
 */
async function open(page) {
  await page.goto('/tests/browser/notes-fixture.html?focus=1');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}
const note = (page) => page.locator('.notes-textarea');

test('opening the notes from the terminal takes the keyboard', async ({ page }) => {
  await open(page);
  await page.locator('textarea.xterm-helper-textarea').focus();
  await page.evaluate(() => window.notesFocus.show());
  await expect(note(page)).toBeEnabled();
  await expect(note(page)).toBeFocused();
  // Typing goes straight into the note.
  await page.keyboard.type('typed without a click');
  await expect(note(page)).toHaveValue(/typed without a click/);
});

test('a field being typed in keeps the keyboard', async ({ page }) => {
  await open(page);
  await page.locator('input.typing-field').focus();
  await page.evaluate(() => window.notesFocus.show());
  await expect(note(page)).toBeEnabled();
  await page.waitForTimeout(150);
  await expect(page.locator('input.typing-field')).toBeFocused();
});
