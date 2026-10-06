import { test, expect } from '@playwright/test';

/**
 * Dictating into a field — a task's title in the add-task dialog. The words
 * being heard are offered for showing, the final ones go into the field, and
 * stopping with the dialog still open leaves dictation on the field: it used
 * to go back to the terminal, so the next hotkey press typed into the terminal
 * hidden behind the dialog.
 */
async function open(page) {
  await page.goto('/tests/browser/dictation-field-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

const fixture = (page, fn, ...args) => page.evaluate(([name, a]) => window.dictationFixture[name](...a), [fn, args]);

test('interim words are shown, final ones go into the field @webkit', async ({ page }) => {
  await open(page);
  await fixture(page, 'toggle');
  expect(await fixture(page, 'listening')).toBe(true);
  expect(await fixture(page, 'target')).toBe('field');

  await fixture(page, 'emit', 'dictation:interimText', 'új fela');
  expect(await fixture(page, 'interim')).toBe('új fela');

  await fixture(page, 'emit', 'dictation:fieldText', 'új feladat');
  await expect(page.locator('#title')).toHaveValue('új feladat');
  expect(await fixture(page, 'interim')).toBe('');
});

test('stopping with the dialog open leaves dictation on the field', async ({ page }) => {
  await open(page);
  await fixture(page, 'toggle');
  await fixture(page, 'toggle');
  expect(await fixture(page, 'listening')).toBe(false);
  expect(await fixture(page, 'targetCalls')).not.toContain('terminal');
  expect(await fixture(page, 'target')).toBe('field');
});
