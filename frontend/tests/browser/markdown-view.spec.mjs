import { test, expect } from '@playwright/test';

/**
 * A Markdown file in the Files view is shown rendered, with a switch to its
 * source. Links stay inside the app unless they are web links, images of the
 * repository load through the backend, and HTML in the file cannot run.
 */
async function open(page) {
  await page.goto('/tests/browser/markdown-view-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
  await page.locator('[data-tree-path="README.md"]').click();
  await expect(page.locator('[data-markdown-view] h1')).toHaveText('Fixture Project');
}

test('a Markdown file opens rendered, and switches to its source and back @webkit', async ({ page }) => {
  await open(page);
  const view = page.locator('[data-markdown-view]');
  await expect(view.locator('table td').first()).toHaveText('a');
  await expect(view.locator('input[type="checkbox"]')).toHaveCount(2);
  await expect(page.locator('.editor.read')).toHaveCount(0);

  const toggle = page.locator('[data-markdown-toggle]');
  await toggle.click();
  await expect(view).toHaveCount(0);
  await expect(page.locator('.editor.read .cm-content')).toContainText('# Fixture Project');

  // The choice holds for the next Markdown file too.
  await page.locator('[data-tree-path="docs"]').click();
  await page.locator('[data-tree-path="docs/guide.md"]').click();
  await expect(page.locator('.editor.read .cm-content')).toContainText('## Setup');
  await toggle.click();
  await expect(view.locator('h1')).toHaveText('Guide');
});

test('a file that is not Markdown has no rendered view', async ({ page }) => {
  await open(page);
  await page.locator('[data-tree-path="notes.txt"]').click();
  await expect(page.locator('.editor.read .cm-content')).toContainText('# not markdown');
  await expect(page.locator('[data-markdown-toggle]')).toHaveCount(0);
});

test('HTML in the file cannot run @webkit', async ({ page }) => {
  await open(page);
  const view = page.locator('[data-markdown-view]');
  await expect(view.locator('script')).toHaveCount(0);
  await expect(view.locator('[onerror]')).toHaveCount(0);
  await view.getByText('evil').click();
  expect(await page.evaluate(() => window.__pwned)).toBeUndefined();
  expect(await page.evaluate(() => window.markdownFixture.openedURLs())).toEqual([]);
});

test('images of the repository load through the backend, and only from inside it @webkit', async ({ page }) => {
  await open(page);
  const logo = page.locator('[data-markdown-view] img[alt="Logo"]');
  await expect(logo).toHaveAttribute('src', /^data:image\/png;base64,/);
  await expect(page.locator('[data-markdown-view] img[alt="Outside"]')).toHaveClass(/broken/);
  // The raw <img src="x"> is asked for too — inside the root, the backend is
  // what decides it is not an image. The one climbing out is never asked for.
  expect(await page.evaluate(() => window.markdownFixture.imageCalls())).toEqual(['docs/logo.png', 'x']);
});

test('links go to the browser, a heading, or another file of the repository', async ({ page }) => {
  await open(page);
  const view = page.locator('[data-markdown-view]');

  await view.getByRole('link', { name: 'site' }).click();
  expect(await page.evaluate(() => window.markdownFixture.openedURLs())).toEqual(['https://example.com/site']);

  // The second "Usage" heading, as GitHub numbers repeated ones.
  const second = view.locator('h2').nth(1);
  await expect(second).not.toBeInViewport();
  await view.getByRole('link', { name: 'usage' }).click();
  await expect(second).toBeInViewport();

  await view.getByRole('link', { name: 'guide' }).click();
  await expect(view.locator('h1')).toHaveText('Guide');
  await expect(page.locator('[data-tree-path="docs/guide.md"]')).toBeVisible();
});

test('Ctrl+F searches the rendered text, and Escape leaves it as it was @webkit', async ({ page }) => {
  await open(page);
  const view = page.locator('[data-markdown-view]');
  const before = await view.locator('.markdown-body').innerHTML();
  await view.locator('h1').click();
  await page.keyboard.press('Control+f');

  // Still the rendered page, with the find bar over it.
  const field = view.locator('.diff-find input');
  await expect(field).toBeFocused();
  await expect(page.locator('.editor.read')).toHaveCount(0);

  await field.fill('usage');
  // The link text, and the two headings.
  await expect(view.locator('mark.find-hit')).toHaveCount(3);
  await expect(view.locator('.find-count')).toHaveText('1/3');
  await expect(view.locator('mark.find-hit.current')).toHaveText('usage');

  await field.press('Enter');
  await field.press('Enter');
  await expect(view.locator('.find-count')).toHaveText('3/3');
  const third = view.locator('mark.find-hit').nth(2);
  await expect(third).toHaveClass(/current/);
  await expect(third).toBeInViewport();

  await field.press('Shift+Enter');
  await expect(view.locator('.find-count')).toHaveText('2/3');

  await field.fill('no such text');
  await expect(view.locator('mark.find-hit')).toHaveCount(0);

  await field.press('Escape');
  await expect(view.locator('.diff-find')).toHaveCount(0);
  expect(await view.locator('.markdown-body').innerHTML()).toBe(before);
});

// The source view's find bar is CodeMirror's search behind the diff's look:
// one bar everywhere, down to the close button.
test('the source view\'s find bar looks like the rendered view\'s @webkit', async ({ page }) => {
  await open(page);
  const look = (selector) => page.locator(selector).evaluate((el) => {
    const s = getComputedStyle(el);
    return [s.paddingTop, s.paddingRight, s.borderTopWidth, s.borderTopColor, s.borderRadius,
      s.fontSize, s.color, s.backgroundColor, s.lineHeight].join(' ');
  });

  await page.locator('[data-markdown-view] h1').click();
  await page.keyboard.press('Control+f');
  const rendered = {
    field: await look('[data-markdown-view] .diff-find input'),
    close: await look('[data-markdown-view] .diff-find button:last-child'),
    count: await look('[data-markdown-view] .diff-find .find-count'),
  };
  await page.keyboard.press('Escape');

  await page.locator('[data-markdown-toggle]').click();
  await expect(page.locator('.editor.read .cm-content')).toBeVisible();
  await page.keyboard.press('Control+f');
  await expect(page.locator('.asmgr-find input[name="search"]')).toBeFocused();
  expect({
    field: await look('.asmgr-find input[name="search"]'),
    close: await look('.asmgr-find button[name="close"]'),
    count: await look('.asmgr-find .find-count'),
  }).toEqual(rendered);
  await expect(page.locator('.asmgr-find button[name="close"]')).toHaveText('×');
});

test('the source view\'s find bar counts, steps, takes options and closes', async ({ page }) => {
  await open(page);
  await page.locator('[data-markdown-toggle]').click();
  await expect(page.locator('.editor.read .cm-content')).toBeVisible();
  await page.keyboard.press('Control+f');
  const bar = page.locator('.asmgr-find');
  const field = bar.locator('input[name="search"]');
  await expect(field).toBeFocused();

  // In the source: the link's text and its #usage-1 target, and the two
  // "## Usage" headings.
  await field.fill('usage');
  await expect(bar.locator('.find-count')).toHaveText('1/4');
  await field.press('Enter');
  await expect(bar.locator('.find-count')).toHaveText('2/4');
  await field.press('Shift+Enter');
  await expect(bar.locator('.find-count')).toHaveText('1/4');

  const matchCase = bar.locator('button[name="caseSensitive"]');
  await matchCase.click();
  await expect(matchCase).toHaveAttribute('aria-pressed', 'true');
  await expect(bar.locator('.find-count')).toHaveText(/\/2$/);

  await field.fill('nothing like this');
  await expect(bar.locator('button[name="next"]')).toBeDisabled();
  // The read view has nothing to replace.
  await expect(bar.locator('input[name="replace"]')).toHaveCount(0);

  await field.press('Escape');
  await expect(bar).toHaveCount(0);
});

test('a jump to a line of a Markdown file shows the source', async ({ page }) => {
  await open(page);
  await page.evaluate(() => window.markdownFixture.requestFileJump('docs/guide.md', 3));
  await expect(page.locator('.editor.read .cm-content')).toContainText('## Setup');
  await expect(page.locator('[data-markdown-view]')).toHaveCount(0);
});

// Ctrl+F used to need a click into the text first: the view had no focus of
// its own after a file was opened from the tree, or after a switch to it.
test('Ctrl+F works straight after opening a file from the tree', async ({ page }) => {
  await open(page);
  await page.locator('[data-tree-path="notes.txt"]').click();
  await expect(page.locator('.editor.read .cm-content')).toContainText('# not markdown');
  await page.keyboard.press('Control+f');
  await expect(page.locator('.asmgr-find input[name="search"]')).toBeFocused();
});

test('Ctrl+F reaches the view with the focus nowhere', async ({ page }) => {
  await open(page);
  await page.evaluate(() => (document.activeElement instanceof HTMLElement) && document.activeElement.blur());
  expect(await page.evaluate(() => document.activeElement === document.body)).toBe(true);
  await page.keyboard.press('Control+f');
  await expect(page.locator('[data-markdown-view] .diff-find input')).toBeFocused();
});

test('in the edit view the find bar replaces too', async ({ page }) => {
  await open(page);
  await page.locator('[data-tree-path="notes.txt"]').click();
  await page.getByRole('button', { name: /^(Edit|Szerkesztés)$/ }).click();
  const editor = page.locator('.editor:not(.read) .cm-content');
  await expect(editor).toContainText('# not markdown');
  await editor.click();
  await page.keyboard.press('Control+f');
  const bar = page.locator('.asmgr-find');
  await bar.locator('input[name="search"]').fill('not');
  await bar.locator('input[name="replace"]').fill('now');
  await bar.locator('button[name="replaceAll"]').click();
  await expect(editor).toContainText('# now markdown');
});
