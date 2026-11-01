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
  await expect(rows(page).nth(1).locator('.group-subtitle')).toHaveText('Clients · web server · bbbbbbbb');
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
  await expect(page.locator('[data-scan-status]')).toContainText('This computer');
  await expect(page.locator('[data-scan-status]')).toContainText('12/40');

  const before = await page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'GetTokenUsage').length);
  await page.evaluate(() => window.tokenUsageFixture.emit('tokenUsage:updated'));
  await expect(page.locator('[data-action="refresh"]')).toBeEnabled();
  await expect(page.locator('[data-scan-status]')).not.toContainText('12/40');
  await expect.poll(() => page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'GetTokenUsage').length)).toBe(before + 1);
});

test('the table fills up while the logs are read, without reloading on every batch', async ({ page }) => {
  await open(page, '?hold');
  await expect(page.locator('[data-action="refresh"]')).toBeDisabled();
  const loads = () => page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'GetTokenUsage').length);
  const progress = (done) => page.evaluate((d) => window.tokenUsageFixture.emit('tokenUsage:progress', { machine: 'local', done: d, total: 40 }), done);
  const before = await loads();

  await progress(1);
  await expect.poll(loads).toBe(before + 1);
  // A burst of batches is one reload.
  await progress(2);
  await progress(3);
  await page.waitForTimeout(100);
  expect(await loads()).toBe(before + 1);
  // A reload during the read leaves it reading.
  await expect(page.locator('[data-action="refresh"]')).toBeDisabled();
  await expect(page.locator('[data-scan-status]')).toContainText('3/40');

  await page.waitForTimeout(3000);
  await progress(4);
  await expect.poll(loads).toBe(before + 2);
});

test('a machine that failed says so beside its last numbers', async ({ page }) => {
  await open(page);
  await page.locator('[data-action="machines"]').click();
  const server = page.locator('[data-machine="srv-1"]');
  await expect(server).toHaveClass(/failed/);
  await expect(server.locator('.machine-error')).not.toHaveText('');
  await expect(server.locator('.machine-error')).not.toContainText('error.');
  await expect(rows(page).nth(1).locator('.group-title')).toHaveText('Site');
});

test('a machine turned off leaves the totals and is not read', async ({ page }) => {
  await open(page);
  await expect(rows(page)).toHaveCount(3);
  await page.locator('[data-action="machines"]').click();
  const server = page.locator('[data-machine="srv-1"]');
  const toggle = server.getByRole('checkbox');
  await expect(toggle).toBeChecked();

  await toggle.uncheck();
  await expect(rows(page)).toHaveCount(2);
  await expect(server).toHaveClass(/off/);
  await expect(server).toContainText('Not read');
  // Its failure is no longer news.
  await expect(server.locator('.machine-error')).toHaveCount(0);
  await expect(rows(page).nth(0).locator('[data-share]')).toHaveText('88.9%');
  // With one machine left there is nothing to filter between.
  await expect(page.locator('[data-control="machine"]')).toHaveCount(0);
  const calls = await page.evaluate(() => window.tokenUsageFixture.calls());
  expect(calls.filter((c) => c.method === 'SetTokenUsageMachineEnabled').at(-1).args).toEqual(['srv-1', false]);
});

test('turning a machine back on reads it again', async ({ page }) => {
  await open(page);
  await page.locator('[data-action="machines"]').click();
  const toggle = page.locator('[data-machine="srv-1"]').getByRole('checkbox');
  await toggle.uncheck();
  await expect(rows(page)).toHaveCount(2);
  const refreshes = () => page.evaluate(() => window.tokenUsageFixture.calls().filter((c) => c.method === 'RefreshTokenUsage').length);
  const before = await refreshes();

  await toggle.check();
  await expect(rows(page)).toHaveCount(3);
  await expect.poll(refreshes).toBe(before + 1);
});

test('looking at one machine ends when it is turned off', async ({ page }) => {
  await open(page);
  await page.locator('[data-control="machine"] .select-trigger').click();
  await page.locator('body > .select-dropdown').getByRole('button', { name: 'web server' }).click();
  await expect(rows(page)).toHaveCount(1);
  await page.locator('[data-action="machines"]').click();
  await page.locator('[data-machine="srv-1"]').getByRole('checkbox').uncheck();
  await expect(rows(page)).toHaveCount(2);
});

test('a switch that fails says so and shows the truth', async ({ page }) => {
  await open(page, '?failToggle');
  await page.locator('[data-action="machines"]').click();
  const toggle = page.locator('[data-machine="srv-1"]').getByRole('checkbox');
  // click, not uncheck: uncheck refuses a box that springs back, which here is
  // the behaviour being tested.
  await toggle.click();
  await expect(page.locator('.error')).toBeVisible();
  await expect(toggle).toBeChecked();
});

test('the filter finds a session by name and keeps its share of everything', async ({ page }) => {
  await open(page);
  const search = page.locator('[data-control="search"]');
  await search.fill('site');
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).nth(0).locator('.group-title')).toHaveText('Site');
  await expect(rows(page).nth(0).locator('[data-share]')).toHaveText('18.2%');
  await expect(page.locator('[data-stat="value"] .stat-value')).toHaveText('1.1 k');
  await search.fill('');
  await expect(rows(page)).toHaveCount(3);
});

test('searching for a conversation id opens its row', async ({ page }) => {
  await open(page);
  await page.locator('[data-control="search"]').fill('aaaaaaaa-2');
  await expect(rows(page)).toHaveCount(1);
  await expect(page.locator('[data-conversation="aaaaaaaa-2"]')).toBeVisible();
  await expect(rows(page).nth(0)).toHaveAttribute('aria-expanded', 'true');
});

test('a search that finds nothing says so', async ({ page }) => {
  await open(page);
  await page.locator('[data-control="search"]').fill('nothing like it');
  await expect(page.locator('[data-empty]')).toHaveText('Nothing matches this search.');
});

test('a row of one conversation shows its id', async ({ page }) => {
  await open(page);
  await expect(rows(page).nth(1).locator('.group-subtitle')).toContainText('bbbbbbbb');
});

async function groupBy(page, name) {
  await page.locator('[data-control="group"] .select-trigger').click();
  await page.locator('body > .select-dropdown').getByRole('button', { name, exact: true }).click();
}

test('rows put in one project add up across machines', async ({ page }) => {
  await open(page);
  await rows(page).nth(0).click();
  const input = page.locator('.link-row input');
  await input.fill('Sparrow');
  await input.press('Enter');
  await expect(rows(page).nth(0).locator('.group-subtitle')).toContainText('Sparrow');

  // The other row, on the server: the name is offered, one click away.
  await rows(page).nth(0).click();
  await rows(page).nth(1).click();
  await page.locator('[data-link-name="Sparrow"]').click();
  await expect(rows(page).nth(1).locator('.group-subtitle')).toContainText('Sparrow');

  await groupBy(page, 'Project');
  await expect(rows(page).nth(0).locator('.group-title')).toHaveText('Sparrow');
  await expect(rows(page).nth(0).locator('[data-share]')).toHaveText('90.9%');
  await expect(rows(page)).toHaveCount(2);
});

test('a blank name takes a row out of its project', async ({ page }) => {
  await open(page);
  await rows(page).nth(0).click();
  const input = page.locator('.link-row input');
  await input.fill('Sparrow');
  await input.press('Enter');
  await expect(rows(page).nth(0).locator('.group-subtitle')).toContainText('Sparrow');
  await page.locator('.link-row input').fill('');
  await page.locator('.link-row input').press('Enter');
  await expect(rows(page).nth(0).locator('.group-subtitle')).toContainText('Default');
});

test('the link is offered only where a row is a session', async ({ page }) => {
  await open(page);
  await groupBy(page, 'Model');
  await rows(page).nth(0).click();
  await expect(page.locator('.conversation-row').first()).toBeVisible();
  await expect(page.locator('.link-row')).toHaveCount(0);
});

test('twenty machines stay out of the way until asked for', async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 600 });
  await open(page, '?many');
  const table = page.locator('.table');
  const button = page.locator('[data-action="machines"]');
  await expect(button).toHaveText('Machines: 22/22');
  await expect(page.locator('[data-machines]')).toHaveCount(0);
  const before = (await table.boundingBox()).y;

  await button.click();
  const panel = page.locator('[data-machines]');
  await expect(panel.locator('.machine')).toHaveCount(22);
  // It lies over the table and scrolls by itself, whatever the number.
  expect((await table.boundingBox()).y).toBe(before);
  const box = await panel.boundingBox();
  expect(box.height).toBeLessThanOrEqual(320);
  expect(await panel.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);
});

test('the machine list closes from outside and with Escape, not from inside', async ({ page }) => {
  await open(page);
  const button = page.locator('[data-action="machines"]');
  const panel = page.locator('[data-machines]');
  await button.click();
  await page.locator('[data-machine="local"] .machine-name').click();
  await expect(panel).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(panel).toHaveCount(0);
  await button.click();
  await page.locator('.note').click();
  await expect(panel).toHaveCount(0);
});

test('machines with errors are counted in the view and open the list', async ({ page }) => {
  await open(page, '?many');
  const link = page.locator('[data-action="show-failed"]');
  await expect(link).toHaveText('5 with errors');
  await link.click();
  await expect(page.locator('[data-machines]')).toBeVisible();
});

test('a machine turned off no longer counts as failed', async ({ page }) => {
  await open(page);
  await expect(page.locator('[data-action="show-failed"]')).toHaveText('1 with errors');
  await page.locator('[data-action="show-failed"]').click();
  await page.locator('[data-machine="srv-1"]').getByRole('checkbox').uncheck();
  await expect(page.locator('[data-action="show-failed"]')).toHaveCount(0);
  await expect(page.locator('[data-action="machines"]')).toHaveText('Machines: 1/2');
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
