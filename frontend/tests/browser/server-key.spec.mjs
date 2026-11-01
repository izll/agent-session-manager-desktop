import { test, expect } from '@playwright/test';

async function openKeyField(page) {
  await page.goto('/tests/browser/server-key-fixture.html');
  await page.waitForSelector('body[data-fixture-ready="true"]');
  await page.getByRole('button', { name: 'Add server' }).first().click();
  const auth = page.locator('.field').filter({ hasText: 'Authentication' }).locator('.select-trigger');
  await auth.click();
  await page.locator('body > .select-dropdown button').filter({ hasText: /key/i }).first().click();
  return page.locator('#server-key-path');
}

test('the chosen key lands in the field', async ({ page }) => {
  const field = await openKeyField(page);
  await field.fill('~/.ssh/old');
  await page.locator('[data-action="browse-key"]').click();
  await expect.poll(() => page.evaluate(() => window.serverKeyFixture.calls())).toEqual([['~/.ssh/old']]);
  await page.evaluate(() => window.serverKeyFixture.answer('~/.ssh/id_rsa'));
  await expect(field).toHaveValue('~/.ssh/id_rsa');
});

test('cancelling leaves the field alone', async ({ page }) => {
  const field = await openKeyField(page);
  await field.fill('~/.ssh/old');
  await page.locator('[data-action="browse-key"]').click();
  await page.evaluate(() => window.serverKeyFixture.answer(''));
  await page.waitForTimeout(50);
  await expect(field).toHaveValue('~/.ssh/old');
});

test('what was typed while the dialog was open stands', async ({ page }) => {
  const field = await openKeyField(page);
  await page.locator('[data-action="browse-key"]').click();
  await field.fill('~/.ssh/typed');
  await page.evaluate(() => window.serverKeyFixture.answer('~/.ssh/id_rsa'));
  await page.waitForTimeout(50);
  await expect(field).toHaveValue('~/.ssh/typed');
});

test('the label still points at the field', async ({ page }) => {
  await openKeyField(page);
  await page.locator('label[for="server-key-path"]').click();
  await expect(page.locator('#server-key-path')).toBeFocused();
});

test('a dialog that fails says so', async ({ page }) => {
  const field = await openKeyField(page);
  await field.fill('~/.ssh/old');
  await page.locator('[data-action="browse-key"]').click();
  await page.evaluate(() => window.serverKeyFixture.refuse('no dialog available'));
  await expect(page.getByText('no dialog available')).toBeVisible();
  await expect(field).toHaveValue('~/.ssh/old');
});
