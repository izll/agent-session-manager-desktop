import { test, expect } from '@playwright/test';

async function open(page, query = '') {
  await page.goto(`/tests/browser/token-usage-fixture.html${query}`);
  await page.waitForSelector('body[data-fixture-ready="true"]');
}

const rows = (page) => page.locator('.group-row');

test('sessions are listed largest first with their share @webkit', async ({ page }) => {
  await open(page);
  await expect(rows(page)).toHaveCount(3);
  await expect(rows(page).nth(0).locator('.group-title')).toHaveText('API refactor');
  await expect(rows(page).nth(0).locator('[data-share]')).toHaveText('72.7%');
  await expect(rows(page).nth(1).locator('.group-title')).toHaveText('Site');
  await expect(rows(page).nth(1).locator('.group-subtitle')).toHaveText('Clients · web server');
  await expect(rows(page).nth(1).locator('[data-share]')).toHaveText('18.2%');
});

test('a conversation in no session is named by its directory', async ({ page }) => {
  await open(page);
  const outside = rows(page).nth(2);
  await expect(outside.locator('.group-title')).toHaveText('me/scratch');
  await expect(outside.locator('.group-subtitle')).toContainText('Not in any session');
});

test('cache reads stay out of the share until asked for', async ({ page }) => {
  await open(page);
  await expect(page.locator('[data-stat="value"] .stat-value')).toHaveText('1.1 k');
  await page.locator('[data-control="basis"] .select-trigger').click();
  await page.locator('body > .select-dropdown').getByRole('button', { name: 'All tokens' }).click();
  await expect(page.locator('[data-stat="value"] .stat-value')).toHaveText('901 k');
  await expect(rows(page).nth(0).locator('[data-share]')).toHaveText('100.0%');
});

test('the summary shows the subagents\' part', async ({ page }) => {
  await open(page);
  await expect(page.locator('[data-stat="subagents"] .stat-value')).toHaveText('9.1%');
  await expect(rows(page).nth(0).locator('.col-num').nth(4)).toHaveText('12.5%');
});

test('grouping by machine, project and model regroups the same numbers', async ({ page }) => {
  await open(page);
  const pick = async (name) => {
    await page.locator('[data-control="group"] .select-trigger').click();
    await page.locator('body > .select-dropdown').getByRole('button', { name, exact: true }).click();
  };
  await pick('Machine');
  await expect(rows(page)).toHaveCount(2);
  await expect(rows(page).nth(0).locator('.group-title')).toHaveText('This computer');
  await expect(rows(page).nth(1).locator('.group-title')).toHaveText('web server');

  await pick('Project');
  await expect(rows(page)).toHaveCount(3);
  await expect(rows(page).nth(0).locator('.group-title')).toHaveText('Default');

  await pick('Model');
  await expect(rows(page)).toHaveCount(2);
  await expect(rows(page).nth(1).locator('.group-title')).toHaveText('gpt-6');
});

test('a machine can be looked at alone', async ({ page }) => {
  await open(page);
  await page.locator('[data-control="machine"] .select-trigger').click();
  await page.locator('body > .select-dropdown').getByRole('button', { name: 'web server' }).click();
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).nth(0).locator('[data-share]')).toHaveText('100.0%');
});

test('a row opens to its conversations', async ({ page }) => {
  await open(page);
  await expect(page.locator('.conversation-row')).toHaveCount(0);
  await rows(page).nth(0).click();
  await expect(page.locator('.conversation-row')).toHaveCount(2);
  await expect(page.locator('[data-conversation="aaaaaaaa-1"] .group-title')).toHaveText('aaaaaaaa');
  await expect(page.locator('[data-conversation="aaaaaaaa-1"] .group-subtitle')).toContainText('claude-opus-5-5');
  await expect(page.locator('[data-conversation="aaaaaaaa-2"] .group-title')).toHaveText('review');
  await rows(page).nth(0).click();
  await expect(page.locator('.conversation-row')).toHaveCount(0);
});

test('choosing a period asks for it', async ({ page }) => {
  await open(page);
  await expect(rows(page)).toHaveCount(3);
  await page.locator('[data-period="1"]').click();
  await expect(rows(page)).toHaveCount(1);
  const asked = await page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'GetTokenUsage').map((c) => c.args[0]));
  expect(asked[0]).toBe(7);
  expect(asked.at(-1)).toBe(1);
});

test('opening the view starts a scan and shows what is counted meanwhile', async ({ page }) => {
  await open(page, '?hold');
  await expect(rows(page)).toHaveCount(3);
  await expect.poll(() => page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'RefreshTokenUsage').length)).toBe(1);
  await expect(page.locator('[data-action="refresh"]')).toBeDisabled();

  await page.evaluate(() => window.tokenUsageFixture.emit('tokenUsage:progress', { machine: 'local', done: 12, total: 40 }));
  await expect(page.locator('[data-machine="local"]')).toContainText('12/40');

  const before = await page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'GetTokenUsage').length);
  await page.evaluate(() => window.tokenUsageFixture.emit('tokenUsage:updated'));
  await expect(page.locator('[data-action="refresh"]')).toBeEnabled();
  await expect(page.locator('[data-machine="local"]')).not.toContainText('12/40');
  await expect.poll(() => page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'GetTokenUsage').length)).toBe(before + 1);
});

test('a machine that failed says so beside its last numbers', async ({ page }) => {
  await open(page);
  const server = page.locator('[data-machine="srv-1"]');
  await expect(server).toHaveClass(/failed/);
  await expect(server.locator('.machine-error')).not.toHaveText('');
  await expect(server.locator('.machine-error')).not.toContainText('error.');
  await expect(rows(page).nth(1).locator('.group-title')).toHaveText('Site');
});

test('no usage says so instead of an empty table', async ({ page }) => {
  await open(page, '?empty');
  await expect(page.locator('[data-empty]')).toHaveText('No token usage in this period.');
});

test('the note says what the share is not', async ({ page }) => {
  await open(page);
  await expect(page.locator('.note')).toContainText('not an exact part');
});

test('the view fits a narrow window without a page scrollbar', async ({ page }) => {
  await page.setViewportSize({ width: 700, height: 500 });
  await open(page);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});
