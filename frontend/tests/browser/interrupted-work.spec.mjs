import { test, expect } from '@playwright/test';

// After a reboot: the sessions that were running are offered back, each with
// only the tabs that were running. What the user sees, what a choice sends to
// the backend, and what happens when one of them fails.

async function open(page, query = '') {
  await page.goto(`/tests/browser/interrupted-work-fixture.html${query}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

const row = (page, id) => page.locator(`.session-row[data-session-id="${id}"]`);
const calls = (page) => page.evaluate(() => window.interruptedFixture.calls());
const dialog = (page) => page.locator('[data-dialog="interrupted-work"]');

test('lists every interrupted session, all ticked, with how many tabs come back', async ({ page }) => {
  await open(page);
  await expect(page.locator('.dialog-header h2')).toHaveText('Continue where you left off?');
  await expect(page.locator('.session-row .name')).toHaveText(['API refactor', 'Docs', 'Ops on build-box']);
  for (const id of ['api', 'docs', 'ops']) {
    await expect(row(page, id).locator('input[type="checkbox"]')).toBeChecked();
  }
  // Three of five: only what was running comes back.
  await expect(row(page, 'api').locator('[data-tabs]')).toHaveText('3 of 5 tabs');
  await expect(row(page, 'ops').locator('[data-tabs]')).toHaveText('All 2 tabs');
  // One tab says nothing about tabs.
  await expect(row(page, 'docs').locator('[data-tabs]')).toHaveCount(0);
  // An icon per agent that comes back.
  await expect(row(page, 'api').locator('.agents > *')).toHaveCount(2);
  await expect(page.getByRole('button', { name: 'Reopen selected (3)' })).toBeEnabled();
});

test('reopens the ticked ones and forgets the one left unticked', async ({ page }) => {
  await open(page);
  await row(page, 'docs').locator('input[type="checkbox"]').uncheck();
  await page.getByRole('button', { name: 'Reopen selected (2)' }).click();

  await expect(dialog(page)).toHaveCount(0);
  expect(await calls(page)).toEqual([
    { method: 'DismissInterruptedWork', args: [['docs'], 'project-a'] },
    { method: 'ReopenInterruptedSessions', args: [['api', 'ops'], 'project-a'] },
  ]);
  expect(await page.evaluate(() => window.interruptedFixture.done())).toEqual([{ ok: 2, failed: 0 }]);
});

test('one failing does not stop the others, and the dialog stays to say which', async ({ page }) => {
  await open(page, '?fail=api');
  await page.getByRole('button', { name: 'Reopen selected (3)' }).click();

  // Progress per row while the rest are still going.
  await expect(row(page, 'api').locator('.state')).toHaveAttribute('data-state', 'error');
  await expect(row(page, 'ops').locator('.state')).toHaveAttribute('data-state', 'ok');
  await expect(row(page, 'docs').locator('.state')).toHaveAttribute('data-state', 'ok');
  await expect(row(page, 'api').locator('.row-error')).toHaveText('This session has nothing left to reopen');

  await expect(dialog(page)).toBeVisible();
  await expect(page.getByRole('button', { name: /Reopen selected/ })).toHaveCount(0);
  expect(await page.evaluate(() => window.interruptedFixture.done())).toEqual([{ ok: 2, failed: 1 }]);

  await page.locator('.dialog-footer').getByRole('button', { name: 'Close' }).click();
  await expect(dialog(page)).toHaveCount(0);
  // Closing after the run forgets nothing more: it was all decided.
  expect((await calls(page)).map((c) => c.method)).toEqual(['ReopenInterruptedSessions']);
});

test('"Not now" forgets the whole offer and starts nothing', async ({ page }) => {
  await open(page);
  await page.getByRole('button', { name: 'Not now' }).click();
  await expect(dialog(page)).toHaveCount(0);
  expect(await calls(page)).toEqual([
    { method: 'DismissInterruptedWork', args: [['api', 'docs', 'ops'], 'project-a'] },
  ]);
});

test('Escape is "Not now" too', async ({ page }) => {
  await open(page);
  await page.keyboard.press('Escape');
  await expect(dialog(page)).toHaveCount(0);
  expect((await calls(page)).map((c) => c.method)).toEqual(['DismissInterruptedWork']);
});

test('names the project when there are several', async ({ page }) => {
  await open(page, '?project=Client%20work');
  await expect(page.locator('.intro')).toContainText('These sessions in Client work were running');
});
