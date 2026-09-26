import { test, expect } from '@playwright/test';

/**
 * Find works in every diff renderer, not only in two columns.
 *
 * The report was "I can't search in the whole-file diff": the bar existed only
 * in the side-by-side view, and a brand-new (or deleted) file is drawn in one
 * column even with two chosen — so for exactly those files it disappeared.
 */

async function open(page, query) {
  await page.goto(`/tests/browser/diff-find-fixture.html?${query}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  // The file has arrived once its first line is drawn.
  await expect(firstLine(page)).toBeVisible({ timeout: 15_000 });
}

/** The file's first line, in whichever renderer drew it. */
const firstLine = (page) => page.locator(':is(.diff-line, .sbs-line)', { hasText: /line 0$/ }).first();

const findButton = (page) => page.locator('button.nav-btn', { hasText: '⌕' });
const bar = (page) => page.locator('.diff-find');
const counter = (page) => page.locator('.diff-find .find-count');
const current = (page) => page.locator('.hit-current');

/** Ctrl+F from inside the diff, the way the reader reaches for it. */
async function ctrlF(page) {
  await firstLine(page).click();
  await page.keyboard.press('Control+f');
  await expect(bar(page).locator('input')).toBeFocused();
}

async function searchAndWalk(page) {
  await page.keyboard.type('needle');
  await expect(counter(page)).toHaveText('1/2');
  await expect(current(page)).toHaveCount(1);
  await expect(current(page)).toContainText('const needle = 5;');

  // The second match is 245 lines further down — far outside what the
  // virtualised view has in the DOM when the search runs.
  await page.keyboard.press('Enter');
  await expect(counter(page)).toHaveText('2/2');
  await expect(current(page)).toContainText('return NEEDLE_250;');
  await expect(current(page)).toBeInViewport();

  // Wraps, and the arrows and Shift+Enter walk back.
  await page.keyboard.press('ArrowDown');
  await expect(counter(page)).toHaveText('1/2');
  await page.keyboard.press('Shift+Enter');
  await expect(counter(page)).toHaveText('2/2');

  await page.keyboard.type('zzz');
  await expect(counter(page)).not.toHaveText(/\//);
  await expect(current(page)).toHaveCount(0);

  await page.keyboard.press('Escape');
  await expect(bar(page)).toHaveCount(0);
}

for (const view of ['whole', 'hunks']) {
  for (const file of ['added.txt', 'deleted.txt']) {
    test(`diff: ${file} with two columns chosen, ${view} view, can be searched`, async ({ page }) => {
      await open(page, `component=diff&sbs=1&view=${view}&file=${file}`);
      // Drawn in one column despite the choice — the case that lost the bar.
      await expect(page.locator('.sbs-line')).toHaveCount(0);
      await expect(findButton(page)).toBeVisible();
      await ctrlF(page);
      await searchAndWalk(page);
    });
  }

  test(`diff: unified ${view} view can be searched`, async ({ page }) => {
    await open(page, `component=diff&view=${view}&file=added.txt`);
    await findButton(page).click();
    await expect(bar(page).locator('input')).toBeFocused();
    await searchAndWalk(page);
  });
}

test('diff: two columns still search', async ({ page }) => {
  await open(page, 'component=diff&sbs=1&view=whole&file=modified.txt');
  await expect(page.locator('.sbs-line').first()).toBeVisible();
  await findButton(page).click();
  await page.keyboard.type('needle');
  await expect(counter(page)).toHaveText('1/2');
  await page.keyboard.press('Enter');
  await expect(counter(page)).toHaveText('2/2');
});

test('diff: an open search follows to the next file without moving the view', async ({ page }) => {
  await open(page, 'component=diff&sbs=1&view=whole&file=added.txt');
  await ctrlF(page);
  await page.keyboard.type('needle');
  await expect(counter(page)).toHaveText('1/2');

  await page.locator('.file-row', { hasText: 'deleted.txt' }).click();
  await expect(page.getByText('return NEEDLE_250;')).toHaveCount(0);
  // Recounted for the new file; the cursor is on no match until asked.
  await expect(counter(page)).toHaveText('–/2');
  await expect(page.locator('.hit')).not.toHaveCount(0);
  await bar(page).locator('input').press('Enter');
  await expect(counter(page)).toHaveText('1/2');
});

for (const file of ['added.txt', 'deleted.txt']) {
  test(`history: ${file} with two columns chosen can be searched`, async ({ page }) => {
    await open(page, `component=history&sbs=1&view=hunks&file=${file}`);
    await expect(page.locator('.sbs-line')).toHaveCount(0);
    await expect(findButton(page)).toBeVisible();
    await page.keyboard.press('Control+f');
    await expect(bar(page).locator('input')).toBeFocused();
    await page.keyboard.type('needle');
    await expect(counter(page)).toHaveText('1/2');
    // The arrows walk matches, not the commit list, while the field has them.
    await page.keyboard.press('ArrowDown');
    await expect(counter(page)).toHaveText('2/2');
    await expect(current(page)).toContainText('NEEDLE_250');
    await expect(current(page)).toBeInViewport();
    // Escape closes the bar, not the dialog.
    await page.keyboard.press('Escape');
    await expect(bar(page)).toHaveCount(0);
    await expect(page.locator('.dialog-overlay')).toHaveCount(1);
  });
}

test('history: two columns still search', async ({ page }) => {
  await open(page, 'component=history&sbs=1&view=hunks&file=modified.txt');
  await expect(page.locator('.sbs-line').first()).toBeVisible();
  await findButton(page).click();
  await page.keyboard.type('needle');
  await expect(counter(page)).toHaveText('1/2');
});
