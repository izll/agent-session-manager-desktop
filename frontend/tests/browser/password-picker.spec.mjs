import { test, expect } from '@playwright/test';

async function open(page, query = '') {
  await page.goto(`/tests/browser/password-picker-fixture.html${query}`);
  await page.waitForSelector('body[data-fixture-ready="true"]');
}
const calls = (page, method) => page.evaluate((m) => window.passwordFixture.calls().filter((c) => c.method === m), method);

test('a password is picked by search and Enter @webkit', async ({ page }) => {
  await open(page);
  await page.locator('.search').fill('prod');
  await expect(page.locator('.row')).toHaveCount(1);
  await page.keyboard.press('Enter');
  await expect.poll(() => calls(page, 'InsertPassword')).toEqual([
    { method: 'InsertPassword', args: ['local', 'l1', 's1', 0, false, 'p1'] },
  ]);
  await expect(page.locator('[data-dialog="password-picker"]')).toHaveCount(0);
});

test('Enter is pressed only when asked for', async ({ page }) => {
  await open(page);
  await page.locator('.enter-toggle input').check();
  await page.locator('.search').focus();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await calls(page, 'InsertPassword'))[0]?.args).toEqual(['local', 'l2', 's1', 0, true, 'p1']);
});

test('a row under the pointer does not take the selection until the mouse moves', async ({ page }) => {
  await open(page);
  await expect(page.locator('.row.selected')).toHaveAttribute('data-entry', 'Prod DB');
  await page.locator('.row').nth(1).hover();
  await expect(page.locator('.row.selected')).toHaveAttribute('data-entry', 'Mail');
});

test('a tab with an agent asks first, and nothing is typed until confirmed', async ({ page }) => {
  await open(page, '?agent');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-confirm]')).toContainText('model provider');
  expect(await calls(page, 'InsertPassword')).toEqual([]);
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-confirm]')).toHaveCount(0);
  await expect(page.locator('[data-dialog="password-picker"]')).toBeVisible();
  await page.keyboard.press('Enter');
  await page.locator('[data-confirm] button').click();
  await expect.poll(async () => (await calls(page, 'InsertPassword')).length).toBe(1);
});

test('holding Enter does not walk through the warning', async ({ page }) => {
  await open(page, '?agent');
  await page.keyboard.down('Enter');
  await expect(page.locator('[data-confirm]')).toBeVisible();
  for (let n = 0; n < 5; n++) await page.keyboard.down('Enter');
  await page.keyboard.up('Enter');
  expect(await calls(page, 'InsertPassword')).toEqual([]);
  // A separate press is a decision.
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await calls(page, 'InsertPassword')).length).toBe(1);
});

test('clicking the row again does not walk through the warning', async ({ page }) => {
  await open(page, '?agent');
  await page.locator('[data-entry="Mail"]').click();
  await expect(page.locator('[data-confirm]')).toBeVisible();
  await page.locator('[data-entry="Mail"]').click();
  await page.waitForTimeout(100);
  expect(await calls(page, 'InsertPassword')).toEqual([]);
});

// The warning pushes the rows up, so the second click can land on its button.
test('the second click of a double click cannot press the button in the warning', async ({ page }) => {
  await open(page, '?agent');
  await page.evaluate(async () => {
    document.querySelector('[data-entry="Mail"]').click();
    await new Promise((resolve) => setTimeout(resolve, 50));
    document.querySelector('[data-confirm] button').click();
  });
  await page.waitForTimeout(100);
  expect(await calls(page, 'InsertPassword')).toEqual([]);
  await page.locator('[data-confirm] button').click();
  await expect.poll(async () => (await calls(page, 'InsertPassword')).length).toBe(1);
});

// An Enter already held when the dialog opened, as after submitting another prompt.
test('a repeat of an Enter held from before does not type', async ({ page }) => {
  await open(page);
  await page.evaluate(() => {
    document.querySelector('.search').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', repeat: true, bubbles: true, cancelable: true }));
  });
  await page.waitForTimeout(50);
  expect(await calls(page, 'InsertPassword')).toEqual([]);
});

test('a held Enter does not follow the password into the terminal', async ({ page }) => {
  await open(page);
  await page.evaluate(() => {
    window.reached = 0;
    document.addEventListener('keydown', (e) => { if (e.key === 'Enter') window.reached++; });
  });
  await page.keyboard.down('Enter');
  await expect(page.locator('[data-dialog="password-picker"]')).toHaveCount(0);
  // Only what comes after the dialog has gone can reach a terminal.
  await page.evaluate(() => { window.reached = 0; });
  await page.keyboard.down('Enter');
  await page.keyboard.down('Enter');
  expect(await page.evaluate(() => window.reached)).toBe(0);
  await page.keyboard.up('Enter');
  await page.keyboard.press('Enter');
  expect(await page.evaluate(() => window.reached)).toBe(1);
});

test('Enter on a button does what the button does, and types nothing', async ({ page }) => {
  await open(page);
  await page.locator('[data-action="manage"]').focus();
  await page.keyboard.press('Enter');
  await expect.poll(() => page.evaluate(() => window.managerAsked)).toBe(true);
  expect(await calls(page, 'InsertPassword')).toEqual([]);
});

test('Enter on the checkbox types nothing', async ({ page }) => {
  await open(page);
  await page.locator('.enter-toggle input').focus();
  await page.keyboard.press('Enter');
  await page.waitForTimeout(50);
  expect(await calls(page, 'InsertPassword')).toEqual([]);
});

test('a shell tab does not ask', async ({ page }) => {
  await open(page);
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await calls(page, 'InsertPassword')).length).toBe(1);
});

test('a locked vault opens with its master password, which leaves the field', async ({ page }) => {
  await open(page);
  await expect(page.locator('.row')).toHaveCount(2);
  await page.locator('[data-locked="keepassxc"]').click();
  const field = page.locator('[data-unlock="keepassxc"] input');
  await expect(field).toHaveAttribute('type', 'password');
  await field.fill('wrong');
  await page.keyboard.press('Enter');
  await expect(page.locator('.error-line')).toBeVisible();
  await expect(field).toHaveValue('');

  await field.fill('right');
  await page.keyboard.press('Enter');
  await expect(page.locator('.row')).toHaveCount(3);
  await expect(page.locator('[data-entry="Vault entry"] .row-sub')).toHaveText('Work');
  await expect(page.locator('[data-locked="keepassxc"]')).toHaveCount(0);

  // Keys still work once the field has gone.
  await expect(page.locator('.search')).toBeFocused();

  await page.locator('[data-action="lock"]').click();
  await expect(page.locator('.row')).toHaveCount(2);
  await expect(page.locator('[data-locked="keepassxc"]')).toBeVisible();
});

test('Escape steps back from the master password before closing', async ({ page }) => {
  await open(page);
  await page.locator('[data-locked="keepassxc"]').click();
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-unlock="keepassxc"]')).toHaveCount(0);
  await expect(page.locator('[data-dialog="password-picker"]')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-dialog="password-picker"]')).toHaveCount(0);
});

test('a failed insert says so and stays open', async ({ page }) => {
  await open(page, '?failInsert');
  await page.keyboard.press('Enter');
  await expect(page.locator('.error-line')).toBeVisible();
  await expect(page.locator('[data-dialog="password-picker"]')).toBeVisible();
});

test('with no session selected there is nothing to type into, but the list can be managed', async ({ page }) => {
  await open(page, '?nosession');
  await expect(page.locator('.search')).toHaveCount(0);
  await page.locator('[data-action="manage"]').click();
  await expect.poll(() => page.evaluate(() => window.managerAsked)).toBe(true);
});

// --- manager ---

test('a password is added, with the password only ever sent, never shown', async ({ page }) => {
  await open(page, '?manager');
  await page.locator('[data-action="add"]').click();
  const form = page.locator('[data-form]');
  await form.locator('input').nth(0).fill('New one');
  await form.locator('input').nth(1).fill('bob');
  await expect(form.locator('input').nth(2)).toHaveAttribute('type', 'password');
  await form.locator('input').nth(2).fill('se:cret');
  await form.locator('button[type="submit"]').click();
  await expect(page.locator('[data-section="local"] [data-entry="New one"]')).toBeVisible();
  expect((await calls(page, 'SaveLocalPassword'))[0].args).toEqual(['', 'New one', 'bob', 'se:cret']);
  await expect(page.locator('[data-form]')).toHaveCount(0);
});

test('editing does not load the password, and an empty field keeps it', async ({ page }) => {
  await open(page, '?manager');
  await page.locator('[data-entry="Prod DB"] [data-action="edit"]').click();
  const form = page.locator('[data-form]');
  await expect(form.locator('input').nth(0)).toHaveValue('Prod DB');
  await expect(form.locator('input').nth(2)).toHaveValue('');
  await form.locator('input').nth(0).fill('Renamed');
  await form.locator('button[type="submit"]').click();
  await expect(page.locator('[data-entry="Renamed"]')).toBeVisible();
  expect((await calls(page, 'SaveLocalPassword'))[0].args).toEqual(['l1', 'Renamed', 'root', '']);
});

test('deleting asks first', async ({ page }) => {
  await open(page, '?manager');
  await page.locator('[data-entry="Mail"] [data-action="delete"]').click();
  expect(await calls(page, 'DeleteLocalPassword')).toEqual([]);
  await page.getByRole('button', { name: 'Delete', exact: true }).last().click();
  await expect(page.locator('[data-section="local"] [data-entry="Mail"]')).toHaveCount(0);
  expect((await calls(page, 'DeleteLocalPassword'))[0].args).toEqual(['l2']);
});

test('without a keyring it says so and offers no way to add', async ({ page }) => {
  await open(page, '?manager&nokeyring');
  await expect(page.locator('[data-no-keyring]')).toBeVisible();
  await expect(page.locator('[data-action="add"]')).toBeDisabled();
});

test('the KeePassXC database can be browsed for', async ({ page }) => {
  await open(page, '?manager');
  await expect(page.locator('[data-section="keepassxc"] input').first()).toHaveValue('/home/me/v.kdbx');
  await page.locator('[data-action="browse-db"]').click();
  await expect(page.locator('[data-section="keepassxc"] input').first()).toHaveValue('/picked/file.kdbx');
  expect((await calls(page, 'SetKeePassXCDatabase')).at(-1).args).toEqual(['/picked/file.kdbx', '']);
});

test('a missing bw is named, and no account can be added', async ({ page }) => {
  await open(page, '?manager');
  await expect(page.locator('[data-section="bitwarden"] [data-not-installed]')).toContainText('bw');
  await expect(page.locator('[data-action="add-account"]')).toHaveCount(0);
});

test('each server is an account of its own', async ({ page }) => {
  await open(page, '?manager&bw&accounts');
  await expect(page.locator('[data-account="Office"]')).toContainText('vault-a.example.com');
  await expect(page.locator('[data-account="Plant"]')).toContainText('vault-b.example.com');
  await page.locator('[data-action="add-account"]').click();
  await page.locator('[data-field="account-name"]').fill('Third');
  await page.locator('[data-field="account-server"]').fill('https://c.example.com');
  await page.locator('[data-bitwarden-form] button[type="submit"]').click();
  await expect.poll(async () => (await calls(page, 'SaveBitwardenAccount'))[0]?.args).toEqual(['Third', 'https://c.example.com']);
  await expect(page.locator('[data-account="Third"]')).toContainText('c.example.com');
});

test('without an address an account is on bitwarden.com', async ({ page }) => {
  await open(page, '?manager&bw');
  await page.locator('[data-action="add-account"]').click();
  await page.locator('[data-field="account-name"]').fill('Personal');
  await page.locator('[data-bitwarden-form] button[type="submit"]').click();
  await expect(page.locator('[data-account="Personal"]')).toContainText('bitwarden.com');
});

test('a bad address keeps the form open and says why', async ({ page }) => {
  await open(page, '?manager&bw');
  await page.locator('[data-action="add-account"]').click();
  await page.locator('[data-field="account-name"]').fill('x');
  await page.locator('[data-field="account-server"]').fill('bad');
  await page.locator('[data-bitwarden-form] button[type="submit"]').click();
  await expect(page.locator('.error-line')).toBeVisible();
  await expect(page.locator('[data-bitwarden-form]')).toBeVisible();
});

test('an account can be deleted, and only that one', async ({ page }) => {
  await open(page, '?manager&bw&accounts');
  await page.locator('[data-account="Plant"] [data-action="delete-account"]').click();
  await page.locator('.btn-confirm').click();
  await expect.poll(async () => (await calls(page, 'DeleteBitwardenAccount'))[0]?.args).toEqual(['a2']);
  await expect(page.locator('[data-account="Office"]')).toBeVisible();
  expect(await calls(page, 'DeleteLocalPassword')).toEqual([]);
});

test('each locked account asks by its own name in the picker', async ({ page }) => {
  await open(page, '?accounts');
  await expect(page.locator('[data-locked="bitwarden:a1"]')).toContainText('Office');
  await expect(page.locator('[data-locked="bitwarden:a2"]')).toContainText('Plant');
  await page.locator('[data-locked="bitwarden:a2"]').click();
  await page.locator('[data-unlock="bitwarden:a2"] input').fill('right');
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await calls(page, 'UnlockPasswordSource'))[0]?.args).toEqual(['bitwarden:a2', 'right']);
});

test('a preset fills in the commands, and what is saved is what the form shows', async ({ page }) => {
  await open(page, '?manager');
  await page.locator('[data-action="add-custom"]').click();
  await page.locator('[data-preset="gopass"]').click();
  await expect(page.locator('[data-field="list"]')).toHaveValue('gopass ls --flat');
  await page.locator('[data-field="get"]').fill('gopass show {name}');
  await page.locator('[data-custom-form] button[type="submit"]').click();
  await expect.poll(async () => (await calls(page, 'SaveCustomPasswordSource'))[0]?.args).toEqual(['', 'gopass', 'gopass ls --flat', 'gopass show {name}']);
  await expect(page.locator('[data-custom="gopass"]')).toContainText('gopass ls --flat');
  // The tool is not on this machine, and the row says so by name.
  await expect(page.locator('[data-custom="gopass"] [data-not-installed]')).toContainText('gopass');
});

test('the hint shows the placeholder literally', async ({ page }) => {
  await open(page, '?manager');
  await expect(page.locator('[data-section="custom"] .hint')).toContainText('{name}');
});

test('a command the backend refuses keeps the form open and says why', async ({ page }) => {
  await open(page, '?manager');
  await page.locator('[data-action="add-custom"]').click();
  await page.locator('[data-field="name"]').fill('x');
  await page.locator('[data-field="list"]').fill('t ls');
  await page.locator('[data-field="get"]').fill("sh -c 'show {name}'");
  await page.locator('[data-custom-form] button[type="submit"]').click();
  await expect(page.locator('.error-line')).toBeVisible();
  await expect(page.locator('[data-custom-form]')).toBeVisible();
});

test('a custom source can be edited and deleted', async ({ page }) => {
  await open(page, '?manager&custom');
  await page.locator('[data-custom="My tool"] [data-action="edit-custom"]').click();
  await expect(page.locator('[data-field="get"]')).toHaveValue('mytool get {name}');
  await expect(page.locator('[data-preset]')).toHaveCount(0);
  await page.locator('[data-field="name"]').fill('Renamed');
  await page.locator('[data-custom-form] button[type="submit"]').click();
  await expect.poll(async () => (await calls(page, 'SaveCustomPasswordSource'))[0]?.args[0]).toBe('c1');
  await page.locator('[data-custom="Renamed"] [data-action="delete-custom"]').click();
  await page.locator('.btn-confirm').click();
  await expect.poll(async () => (await calls(page, 'DeleteCustomPasswordSource'))[0]?.args).toEqual(['c1']);
  expect(await calls(page, 'DeleteLocalPassword')).toEqual([]);
});

test('a custom source is named in the picker, and offers no locking', async ({ page }) => {
  await open(page, '?custom');
  await expect(page.locator('[data-entry="Tool entry"] .row-source')).toHaveText('My tool');
  await expect(page.locator('[data-action="lock"]')).toHaveCount(0);
  await page.locator('[data-entry="Tool entry"]').click();
  await expect.poll(async () => (await calls(page, 'InsertPassword'))[0]?.args.slice(0, 2)).toEqual(['custom:c1', 'Work/Tool entry']);
});

test('Escape calls off a fetch that is taking long', async ({ page }) => {
  await open(page, '?slowInsert');
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await calls(page, 'InsertPassword')).length).toBe(1);
  await expect(page.locator('[data-dialog="password-picker"]')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect.poll(async () => (await calls(page, 'CancelPasswordInsert')).length).toBe(1);
  await expect(page.locator('[data-dialog="password-picker"]')).toHaveCount(0);
});

test('logging in opens a tab for that account and goes there', async ({ page }) => {
  await open(page, '?manager&bw&accounts');
  await page.locator('[data-account="Plant"] [data-action="bitwarden-login"]').click();
  await expect.poll(async () => (await calls(page, 'OpenBitwardenLogin'))[0]?.args).toEqual(['a2', 's1', 2, 'p1']);
  await expect(page.locator('[data-dialog="password-manager"]')).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => window.passwordFixture.selectedWindow())).toBe(7);
  // The picker must not come back over the tab where the answers are typed.
  expect(await page.evaluate(() => window.doneCalled)).toBeUndefined();
});

test('a refused login stays in the dialog and says why', async ({ page }) => {
  await open(page, '?manager&bw&accounts&remoteTab');
  await page.locator('[data-account="Office"] [data-action="bitwarden-login"]').click();
  await expect(page.locator('.error-line')).toBeVisible();
  await expect(page.locator('[data-dialog="password-manager"]')).toBeVisible();
});

test('there is no way to log in without a tab to open beside', async ({ page }) => {
  await open(page, '?manager&bw&accounts&nosession');
  await expect(page.locator('[data-account="Office"] [data-action="bitwarden-login"]')).toBeDisabled();
});

const release = (page) => page.evaluate(() => window.passwordFixture.release());

test('opening the picker shows that the list is coming', async ({ page }) => {
  await open(page, '?hold=GetPasswords');
  await expect(page.locator('[data-loading] [data-busy]')).toBeVisible();
  // Nothing yet does not mean nothing there.
  await expect(page.locator('[data-empty]')).toHaveCount(0);
  await release(page);
  await expect(page.locator('[data-entry="Mail"]')).toBeVisible();
  await expect(page.locator('[data-loading]')).toHaveCount(0);
  await expect(page.locator('[data-busy]')).toHaveCount(0);
});

test('opening a vault shows that it is working', async ({ page }) => {
  await open(page, '?accounts&hold=UnlockPasswordSource');
  await page.locator('[data-locked="bitwarden:a1"]').click();
  await page.locator('[data-unlock="bitwarden:a1"] input').fill('right');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-unlock="bitwarden:a1"] button [data-busy]')).toBeVisible();
  await release(page);
  await expect(page.locator('[data-busy]')).toHaveCount(0);
});

test('fetching a password shows on its row', async ({ page }) => {
  await open(page, '?hold=InsertPassword');
  await page.locator('[data-entry="Mail"]').click();
  await expect(page.locator('[data-entry="Mail"] [data-busy]')).toBeVisible();
  await expect(page.locator('[data-entry="Prod DB"] [data-busy]')).toHaveCount(0);
  await expect(page.locator('[data-dialog="password-picker"]')).toHaveClass(/busy/);
  await release(page);
  await expect(page.locator('[data-dialog="password-picker"]')).toHaveCount(0);
});

test('the manager shows that it is loading', async ({ page }) => {
  await open(page, '?manager&hold=GetPasswords');
  await expect(page.locator('[data-dialog="password-manager"] .dialog-header [data-busy]')).toBeVisible();
  await release(page);
  await expect(page.locator('[data-busy]')).toHaveCount(0);
  await expect(page.locator('[data-dialog="password-manager"]')).not.toHaveClass(/busy/);
});

test('adding a server shows that bw is being set up', async ({ page }) => {
  await open(page, '?manager&bw&hold=SaveBitwardenAccount');
  await page.locator('[data-action="add-account"]').click();
  await page.locator('[data-field="account-name"]').fill('Office');
  await page.locator('[data-bitwarden-form] button[type="submit"]').click();
  await expect(page.locator('[data-bitwarden-form] button[type="submit"] [data-busy]')).toBeVisible();
  await expect(page.locator('[data-bitwarden-form] button[type="submit"]')).toBeDisabled();
  await release(page);
  await expect(page.locator('[data-account="Office"]')).toBeVisible();
  await expect(page.locator('[data-busy]')).toHaveCount(0);
});

test('opening a login shows on the account it is for', async ({ page }) => {
  await open(page, '?manager&bw&accounts&hold=OpenBitwardenLogin');
  await page.locator('[data-account="Plant"] [data-action="bitwarden-login"]').click();
  await expect(page.locator('[data-account="Plant"] [data-action="bitwarden-login"] [data-busy]')).toBeVisible();
  await expect(page.locator('[data-account="Office"] [data-busy]')).toHaveCount(0);
  await expect(page.locator('[data-account="Office"] [data-action="bitwarden-login"]')).toBeDisabled();
  await release(page);
  await expect(page.locator('[data-dialog="password-manager"]')).toHaveCount(0);
});

test('Enter after the password is remembered, on and off', async ({ page }) => {
  await open(page);
  const toggle = page.locator('.enter-toggle input');
  await expect(toggle).not.toBeChecked();
  await toggle.check();
  await open(page);
  await expect(toggle).toBeChecked();
  await page.locator('[data-entry="Mail"]').click();
  await expect.poll(async () => (await calls(page, 'InsertPassword'))[0]?.args[4]).toBe(true);
  await open(page);
  await toggle.uncheck();
  await open(page);
  await expect(toggle).not.toBeChecked();
});

test('the master field waits with the rest, and Escape still works meanwhile', async ({ page }) => {
  await open(page, '?accounts&hold=UnlockPasswordSource');
  await page.locator('[data-locked="bitwarden:a1"]').click();
  const field = page.locator('[data-unlock="bitwarden:a1"] input');
  await field.fill('right');
  await page.keyboard.press('Enter');
  await expect(field).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(field).toHaveCount(0);
  await release(page);
});

test('a wrong master password gives the field back, ready to type in', async ({ page }) => {
  await open(page, '?accounts&hold=UnlockPasswordSource');
  await page.locator('[data-locked="bitwarden:a1"]').click();
  const field = page.locator('[data-unlock="bitwarden:a1"] input');
  await field.fill('wrong');
  await page.keyboard.press('Enter');
  await expect(field).toBeDisabled();
  await release(page);
  await expect(field).toBeEnabled();
  await expect(field).toBeFocused();
  await page.keyboard.type('right');
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await calls(page, 'UnlockPasswordSource')).at(-1)?.args).toEqual(['bitwarden:a1', 'right']);
});

test('the spinner stands out on a coloured button', async ({ page }) => {
  await open(page, '?accounts&hold=UnlockPasswordSource');
  await page.locator('[data-locked="bitwarden:a1"]').click();
  await page.locator('[data-unlock="bitwarden:a1"] input').fill('right');
  await page.keyboard.press('Enter');
  const spinner = page.locator('[data-unlock="bitwarden:a1"] button [data-busy]');
  await expect(spinner).toHaveCSS('border-top-color', 'rgb(255, 255, 255)');
  await release(page);
});

