import { test, expect } from '@playwright/test';

async function open(page, query = '') {
  await page.goto(`/tests/browser/mobile-settings-fixture.html${query}`);
  await page.waitForSelector('body[data-fixture-ready="true"]');
}
const calls = (page, method) => page.evaluate((m) => window.mobileFixture.calls().filter((c) => c.method === m), method);

test('the switch turns serving on at the port shown', async ({ page }) => {
  await open(page);
  await expect(page.locator('[data-mobile-port]')).toHaveValue('9760');
  await page.locator('[data-mobile-toggle]').click();
  await expect.poll(async () => (await calls(page, 'SetMobileAccess'))[0]?.args).toEqual([true, 9760]);
  await expect(page.locator('[data-mobile-toggle]')).toHaveClass(/active/);
  // Only then is there something to pair with, and the command to reach it.
  await expect(page.locator('[data-mobile-pair]')).toBeVisible();
  await expect(page.locator('[data-mobile-tailscale]')).toContainText('tailscale serve --bg http://127.0.0.1:9760');
});

test('a port in use is said, and the switch stays off', async ({ page }) => {
  await open(page, '?busy');
  await page.locator('[data-mobile-toggle]').click();
  await expect(page.locator('[data-mobile-error]')).toContainText('port already in use');
  await expect(page.locator('[data-mobile-toggle]')).not.toHaveClass(/active/);
  await expect(page.locator('[data-mobile-pair]')).toHaveCount(0);
});

test('the warning is there before anything is turned on', async ({ page }) => {
  await open(page);
  await expect(page.locator('[data-mobile-warning]')).toBeVisible();
  // Translated, not a key.
  await expect(page.locator('[data-mobile-warning]')).not.toContainText('mobile.');
});

test('pairing shows a QR code and when it runs out', async ({ page }) => {
  await open(page, '?running');
  await page.locator('[data-mobile-pair]').click();
  await expect(page.locator('[data-mobile-qr] img')).toBeVisible();
  await expect(page.locator('[data-mobile-qr]')).toContainText('#pair=abc');
  await expect(page.locator('[data-mobile-qr]')).not.toContainText('{time}');
});

test('a device is revoked only after confirming', async ({ page }) => {
  await open(page, '?running&devices');
  await expect(page.locator('[data-mobile-device="iPhone"]')).toBeVisible();
  await page.locator('[data-mobile-device="iPhone"] [data-action="revoke"]').click();
  expect(await calls(page, 'RevokeMobileDevice')).toEqual([]);
  await page.locator('.btn-confirm').click();
  await expect.poll(async () => (await calls(page, 'RevokeMobileDevice'))[0]?.args).toEqual(['d1']);
  await expect(page.locator('[data-mobile-device]')).toHaveCount(0);
});

test('changing the port while serving moves the server', async ({ page }) => {
  await open(page, '?running');
  await page.locator('[data-mobile-port]').fill('9811');
  await page.locator('[data-mobile-port]').press('Enter');
  await expect.poll(async () => (await calls(page, 'SetMobileAccess')).at(-1)?.args).toEqual([true, 9811]);
});

test('a refused new port leaves the switch on the port still served', async ({ page }) => {
  await open(page, '?running&busy');
  await page.locator('[data-mobile-port]').fill('9811');
  await page.locator('[data-mobile-port]').press('Enter');
  await expect(page.locator('[data-mobile-error]')).toContainText('port already in use');
  await expect(page.locator('[data-mobile-toggle]')).toHaveClass(/active/);
  await expect(page.locator('[data-mobile-port]')).toHaveValue('9760');
});

const asks = (page) => page.evaluate(() => window.mobileFixture.setupAsks());

test('the steps show where setup stands, briefly', async ({ page }) => {
  await open(page, '?step=login');
  await expect(page.locator('[data-setup-step="install"]')).toHaveClass(/done/);
  await expect(page.locator('[data-setup-step="login"]')).toHaveClass(/current/);
  await expect(page.locator('[data-setup-step="serve"]')).toHaveClass(/pending/);
  // Nothing is run from here: the current step offers the guide.
  await expect(page.locator('[data-setup-guide]')).toHaveCount(1);
  await expect(page.locator('[data-setup-run]')).toHaveCount(0);
});

test('the guide opens at the current step, with its commands to copy', async ({ page }) => {
  await open(page, '?step=login');
  await page.locator('[data-setup-guide]').click();
  const guide = page.locator('[data-dialog="mobile-setup-guide"]');
  await expect(guide).toBeVisible();
  await expect(guide.locator('[data-guide-step="login"]')).toHaveClass(/current/);
  await expect(guide.locator('[data-guide-step="install"] [data-guide-command]')).toHaveText('curl -fsSL https://tailscale.com/install.sh | sh');
  await expect(guide.locator('[data-guide-step="serve"] [data-guide-command]')).toHaveText('sudo tailscale serve --bg http://127.0.0.1:9760');
  await guide.locator('[data-guide-step="login"] [data-guide-copy]').click();
  await expect.poll(async () => (await calls(page, 'ClipboardSetText'))[0]?.args).toEqual(['sudo tailscale up --operator=$USER']);
  await page.keyboard.press('Escape');
  await expect(guide).toHaveCount(0);
});

test('on a Mac the guide sends you to the app, and runs nothing', async ({ page }) => {
  await open(page, '?step=install&platform=darwin');
  await page.locator('[data-setup-guide]').click();
  const guide = page.locator('[data-dialog="mobile-setup-guide"]');
  await expect(guide.locator('[data-guide-step="install"] [data-guide-command]')).toHaveCount(0);
  await guide.locator('[data-guide-download]').click();
  await expect.poll(async () => (await calls(page, 'BrowserOpenURL'))[0]?.args).toEqual(['https://tailscale.com/download']);
});

test('turning mobile access on is the switch itself', async ({ page }) => {
  await open(page, '?step=enable');
  await page.locator('[data-setup-enable]').click();
  await expect.poll(async () => (await calls(page, 'SetMobileAccess'))[0]?.args).toEqual([true, 9760]);
});

test('checking again asks again', async ({ page }) => {
  await open(page, '?step=login');
  await expect.poll(() => asks(page)).toBeGreaterThan(0);
  const before = await asks(page);
  await page.locator('[data-setup-recheck]').click();
  await expect.poll(() => asks(page)).toBe(before + 1);
});

// Commands run in a terminal elsewhere: the list catches up on its own.
test('until done, the state is read again every few seconds', async ({ page }) => {
  await page.clock.install();
  await open(page, '?step=login');
  await expect.poll(() => asks(page)).toBeGreaterThan(0);
  const before = await asks(page);
  await page.clock.runFor(5100);
  await expect.poll(() => asks(page)).toBeGreaterThan(before);
});

test('once done, it stops asking', async ({ page }) => {
  await page.clock.install();
  await open(page, '?step=phone');
  await expect.poll(() => asks(page)).toBeGreaterThan(0);
  const before = await asks(page);
  await page.clock.runFor(16000);
  expect(await asks(page)).toBe(before);
});

test('when all is done it is one line with the address', async ({ page }) => {
  await open(page, '?step=phone');
  await expect(page.locator('[data-setup-done]')).toContainText('gep.tail1234.ts.net');
  await expect(page.locator('[data-setup-step]')).toHaveCount(0);
  await expect(page.locator('[data-mobile-pair]')).toBeVisible();
});
