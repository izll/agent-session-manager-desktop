import { test, expect } from '@playwright/test';

/**
 * Switching to the diff hands it the keyboard.
 *
 * The report: "when I switch to the diff, it should focus the part that takes
 * the keyboard, because I have to click into it". The focus stayed in the
 * terminal, so the page keys and Ctrl+F did nothing until the diff was clicked.
 */

async function open(page, query) {
  await page.goto(`/tests/browser/diff-find-fixture.html?component=focus&${query}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

const firstLine = (page) => page.locator(':is(.diff-line, .sbs-line)', { hasText: /line 0$/ }).first();
const terminal = (page) => page.locator('textarea.xterm-helper-textarea');

async function show(page, takeFocus = true) {
  await page.evaluate((take) => window.diffFocus.show(take), takeFocus);
  await expect(firstLine(page)).toBeVisible({ timeout: 15_000 });
}

const views = [
  { name: 'whole-file', query: 'view=whole&file=added.txt', scroller: '.virtual-viewport' },
  { name: 'hunks-only', query: 'view=hunks&file=added.txt', scroller: '.diff-content' },
  { name: 'two columns', query: 'sbs=1&view=whole&file=modified.txt', scroller: '.sbs .pane[tabindex]' },
];

for (const view of views) {
  test(`${view.name}: switching from the terminal takes the keyboard`, async ({ page }) => {
    await open(page, view.query);
    await terminal(page).focus();
    await show(page);

    const scroller = page.locator(view.scroller).first();
    await expect(scroller).toBeFocused();
    // The page keys scroll it without a click.
    await page.keyboard.press('PageDown');
    await expect.poll(() => scroller.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
    // And Ctrl+F reaches the diff.
    await page.keyboard.press('Control+f');
    await expect(page.locator('.diff-find input')).toBeFocused();
  });
}

test('a field being typed in keeps the keyboard', async ({ page }) => {
  await open(page, 'view=whole&file=added.txt');
  await page.locator('input.typing-field').focus();
  await show(page);
  await page.waitForTimeout(100);
  await expect(page.locator('input.typing-field')).toBeFocused();
});

test('the diff above a terminal leaves the keyboard in the terminal', async ({ page }) => {
  await open(page, 'view=whole&file=added.txt');
  await terminal(page).focus();
  await show(page, false);
  await page.waitForTimeout(100);
  await expect(terminal(page)).toBeFocused();
});
