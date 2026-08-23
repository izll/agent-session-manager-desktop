import { test, expect } from '@playwright/test';

async function gotoNotesFixture(page) {
  await page.goto('/tests/browser/notes-fixture.html');
  // A cold eight-worker Vite run measured near three seconds and the
  // integration run crossed the generic five-second locator budget. Wait on
  // the fixture's explicit post-mount signal so startup load is not confused
  // with a missing textarea or a Notes regression.
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

async function gotoDialogRacesFixture(page, mode) {
  await page.goto(`/tests/browser/dialog-races-fixture.html?mode=${mode}`);
  // The marker is emitted by the fixture component's onMount callback after
  // its initial Svelte DOM flush. Eight concurrent cold Chromium pages took
  // 1.69s locally; 15s leaves CI/shared-run headroom without confusing Vite
  // startup with a missing dialog (the generic assertion budget is only 5s).
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

async function gotoTaskPanelFixture(page, query = '') {
  await page.goto(`/tests/browser/task-panel-fixture.html${query}`);
  // The marker is emitted by a Svelte onMount -> tick -> animation-frame
  // chain. It distinguishes a cold Vite transform from a TaskPanel render
  // regression without relying on the generic five-second locator timeout.
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

async function gotoTabEditorRacesFixture(page) {
  await page.goto('/tests/browser/tab-editor-races-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

async function gotoGroupProjectFixture(page) {
  await page.goto('/tests/browser/group-project-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

async function gotoFeedbackFixture(page) {
  await page.goto('/tests/browser/feedback-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

async function gotoProjectSettingsFixture(page) {
  await page.goto('/tests/browser/project-settings-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

async function gotoProjectLayoutFixture(page) {
  await page.goto('/tests/browser/project-layout-fixture.html');
  // This fixture compiles the real MainPanel/TabBar graph. A clean npm install
  // measured just over 15 s for the first transform; wait on its explicit
  // post-mount signal with the same cold-start budget as the content fixture.
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
}

test('a replacement notification receives its full visible duration', async ({ page }) => {
  await gotoFeedbackFixture(page);
  await page.locator('#toast-first').click();
  await expect(page.getByRole('alert')).toContainText('First notification');
  await page.waitForTimeout(600);
  await page.locator('#toast-second').click();
  await expect(page.getByRole('alert')).toContainText('Second notification');

  // Past the first notification's original deadline, but comfortably inside
  // the replacement's own 800 ms reading window.
  await page.waitForTimeout(300);
  await expect(page.getByRole('alert')).toContainText('Second notification');
  await expect(page.getByRole('alert')).toHaveCount(0, { timeout: 1_000 });
});

test('an identical repeated notification also restarts its visible duration', async ({ page }) => {
  await gotoFeedbackFixture(page);
  await page.locator('#toast-first').click();
  await page.waitForTimeout(600);
  await page.locator('#toast-first').click();
  // The message prop is identical; only the caller's revision distinguishes
  // this notification from the one whose deadline is about to expire.
  await page.waitForTimeout(300);
  await expect(page.getByRole('alert')).toContainText('First notification');
  await expect(page.getByRole('alert')).toHaveCount(0, { timeout: 1_000 });
});

test('a long notification keeps its dismiss control inside a 300px viewport', async ({ page }) => {
  await page.setViewportSize({ width: 300, height: 500 });
  await gotoFeedbackFixture(page);
  await page.locator('#toast-long').click();
  const toast = page.getByRole('alert');
  const close = toast.locator('.toast-close');
  await expect(toast).toBeVisible();
  const [toastBox, closeBox] = await Promise.all([toast.boundingBox(), close.boundingBox()]);
  expect(toastBox.x).toBeGreaterThanOrEqual(0);
  expect(toastBox.x + toastBox.width).toBeLessThanOrEqual(300);
  expect(closeBox.x + closeBox.width).toBeLessThanOrEqual(300);
});

test('an in-flight undo cannot disable a newer undo offer', async ({ page }) => {
  await gotoFeedbackFixture(page);
  await page.locator('#undo-first').click();
  await page.getByRole('button', { name: /Undo 10/ }).click();
  await expect(page.locator('#undo-calls')).toHaveText('1');

  await page.locator('#undo-second').click();
  await expect(page.getByText('Undo second action')).toBeVisible();
  const undo = page.getByRole('button', { name: /Undo 10/ });
  await expect(undo).toBeEnabled();
  await undo.click();
  await expect(page.locator('#undo-calls')).toHaveText('2');
  await page.evaluate(() => window.dispatchEvent(new Event('feedback-resolve-undos')));
});

test('project switching synchronizes the loaded language with the live translation runtime', async ({ page }) => {
  await gotoProjectSettingsFixture(page);
  await expect(page.locator('#settings-language')).toHaveText('en');
  await expect(page.locator('#runtime-locale')).toHaveText('en');

  await page.locator('#switch-language-project').click();
  await expect(page.locator('#settings-language')).toHaveText('hu');
  await expect(page.locator('#runtime-locale')).toHaveText('hu');
  await expect(page.locator('#translated-save')).toHaveText('Mentés');
});

test('a failed replacement-project settings read cannot retain the old full snapshot', async ({ page }) => {
  await gotoProjectSettingsFixture(page);
  await expect(page.locator('#settings-theme')).toHaveText('old-project-theme');

  await page.locator('#switch-failing-settings-project').click();
  await expect(page.locator('#settings-theme')).toHaveText('violet');
  await expect(page.locator('#settings-language')).toHaveText('en');
  await page.locator('#save-after-settings-failure').click();
  await expect.poll(() => page.evaluate(() => window.projectSettingsFixture.settingsSaves().length)).toBe(0);
});

test('project switching reapplies project-scoped diff and dictation panel geometry', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await gotoProjectLayoutFixture(page);
  await page.locator('.split-btn').filter({ hasText: /Diff/ }).click();
  await expect(page.locator('.diff-above')).toHaveCSS('height', '180px');
  const buffer = page.locator('.dictation-buffer');
  await expect(buffer).toBeVisible();
  await expect(buffer).toHaveCSS('left', '20px');
  await expect(buffer).toHaveCSS('width', '320px');

  await page.evaluate(() => window.projectLayoutFixture.switchProject());
  await expect(page.locator('.diff-above')).toHaveCSS('height', '260px');
  await expect(buffer).toHaveCSS('left', '110px');
  await expect(buffer).toHaveCSS('top', '70px');
  await expect(buffer).toHaveCSS('width', '360px');
  await expect(buffer).toHaveCSS('height', '210px');
  expect(pageErrors).toEqual([]);
});

test('layout gestures cannot save old-project geometry after a project switch', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await gotoProjectLayoutFixture(page);
  await page.locator('.split-btn').filter({ hasText: /Diff/ }).click();
  const splitter = page.locator('.diff-splitter');
  const splitterBox = await splitter.boundingBox();
  await page.mouse.move(splitterBox.x + 10, splitterBox.y + 2);
  await page.mouse.down();
  await page.evaluate(() => window.projectLayoutFixture.switchProject('project-b', 260, 110));
  await page.mouse.up();
  await expect.poll(() => page.evaluate(() => window.projectLayoutFixture.settingsSaves().length)).toBe(0);

  const header = page.locator('.buffer-header');
  const headerBox = await header.boundingBox();
  await page.mouse.move(headerBox.x + 20, headerBox.y + 10);
  await page.mouse.down();
  await page.evaluate(() => window.projectLayoutFixture.switchProject('project-c', 240, 80));
  await page.mouse.up();
  await expect.poll(() => page.evaluate(() => window.projectLayoutFixture.settingsSaves().length)).toBe(0);
  expect(pageErrors).toEqual([]);
});

test('a real Svelte component renders, portals, focuses and reacts in Chromium', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

  await page.goto('/tests/browser/select-fixture.html');
  await page.getByRole('button', { name: 'Alpha' }).click();

  const search = page.locator('.select-search');
  await expect(search).toBeFocused();
  await expect(page.locator('body > .select-dropdown')).toBeVisible();
  await search.fill('gam');
  await search.press('Enter');

  await expect(page.locator('body')).toHaveAttribute('data-selected', 'gamma');
  await expect(page.getByRole('button', { name: 'Gamma' })).toBeVisible();
  expect(pageErrors).toEqual([]);
});

test('ConfirmDialog remains visible when its owning view is hidden', async ({ page }) => {
  await page.goto('/tests/browser/confirm-portal-fixture.html');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  expect(await dialog.evaluate((node) => node.parentElement === document.body)).toBe(true);
  await expect(page.getByRole('button', { name: 'Keep editing' })).toBeVisible();
});

test('modal focus stays trapped and returns to its opener after close', async ({ page }) => {
  await page.goto('/tests/browser/confirm-portal-fixture.html');
  const dialog = page.getByRole('dialog');
  const keepEditing = dialog.getByRole('button', { name: 'Keep editing' });
  const discard = dialog.getByRole('button', { name: 'Discard' });
  await expect(keepEditing).toBeFocused();

  await page.keyboard.press('Shift+Tab');
  await expect(discard).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(keepEditing).toBeFocused();
  await expect(page.getByRole('button', { name: 'Background action' })).not.toBeFocused();

  await keepEditing.click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Open confirmation' })).toBeFocused();
});

test('Enter on the focused safe confirmation button cannot trigger the destructive action', async ({ page }) => {
  await page.goto('/tests/browser/confirm-portal-fixture.html');
  const keepEditing = page.getByRole('button', { name: 'Keep editing' });
  await expect(keepEditing).toBeFocused();
  await keepEditing.press('Enter');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('body')).toHaveAttribute('data-cancelled', 'true');
  await expect(page.locator('body')).toHaveAttribute('data-confirmed', 'false');
});

test('late tab editor saves cannot close a replacement draft', async ({ page }) => {
  await gotoTabEditorRacesFixture(page);
  const names = page.locator('.tab-name');

  await names.nth(0).dblclick();
  let renameInput = page.locator('.tab-rename-input');
  await renameInput.fill('delayed first rename');
  await renameInput.press('Enter');
  await expect.poll(() => page.evaluate(() => window.tabEditorRacesFixture.renameCalls())).toBe(1);
  await renameInput.press('Escape');
  await names.nth(1).dblclick();
  renameInput = page.locator('.tab-rename-input');
  await renameInput.fill('replacement rename draft');
  await page.evaluate(() => window.tabEditorRacesFixture.resolveRename(0));
  await expect(renameInput).toBeVisible();
  await expect(renameInput).toHaveValue('replacement rename draft');
  await renameInput.press('Escape');

  await page.locator('.tab').nth(0).click({ button: 'right' });
  await page.getByRole('button', { name: 'Edit Extra Args' }).click();
  let argsInput = page.locator('.extra-args-input');
  await argsInput.fill('--delayed-first');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect.poll(() => page.evaluate(() => window.tabEditorRacesFixture.extraArgsCalls())).toBe(1);
  await page.locator('.extra-args-dialog .close-btn').click();
  await page.locator('.tab').nth(1).click({ button: 'right' });
  await page.getByRole('button', { name: 'Edit Extra Args' }).click();
  argsInput = page.locator('.extra-args-input');
  await argsInput.fill('--replacement-draft');
  await page.evaluate(() => window.tabEditorRacesFixture.resolveExtraArgs(0));
  await expect(argsInput).toBeVisible();
  await expect(argsInput).toHaveValue('--replacement-draft');

  // The editor is modal: keyboard traversal must wrap inside it rather than
  // activating a tab or control underneath the overlay.
  const closeArgs = page.locator('.extra-args-dialog .close-btn');
  await closeArgs.evaluate((button) => button.focus());
  await expect(closeArgs).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(page.locator('.extra-args-actions .btn-primary')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(closeArgs).toBeFocused();
});

test('TaskPanel keeps metadata on one right-aligned row with optional badges', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await gotoTaskPanelFixture(page);
  await expect(page.locator('.task-item')).toHaveCount(4);

  const geometry = await page.locator('.task-item').evaluateAll((items) => items.map((item) => {
    const title = item.querySelector('.task-title-row').getBoundingClientRect();
    const priority = item.querySelector('.priority-badge').getBoundingClientRect();
    const status = item.querySelector('.status-badge').getBoundingClientRect();
    return {
      rowHeight: title.height,
      priorityRight: priority.right,
      statusRight: status.right,
      priorityTop: priority.top,
      statusTop: status.top,
      statusHeight: status.height,
    };
  }));

  expect(Math.max(...geometry.map((row) => row.rowHeight))).toBeLessThanOrEqual(24);
  expect(new Set(geometry.map((row) => Math.round(row.priorityRight))).size).toBe(1);
  expect(new Set(geometry.map((row) => Math.round(row.statusRight))).size).toBe(1);
  for (const row of geometry) {
    expect(Math.abs(row.priorityTop - row.statusTop)).toBeLessThanOrEqual(1);
    expect(row.statusHeight).toBeLessThanOrEqual(20);
  }
  expect(pageErrors).toEqual([]);
});

test('TaskPanel modal focus is contained and Escape closes it', async ({ page }) => {
  await gotoTaskPanelFixture(page);
  await page.getByRole('button', { name: /Add Task|Feladat hozzáadása/ }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect.poll(() => dialog.evaluate((node) => node.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
});

test('TaskPanel keeps trailing status and priority inside a 520px combined-metadata row', async ({ page }) => {
  await gotoTaskPanelFixture(page);
  await expect(page.locator('.task-item')).toHaveCount(4);
  await page.locator('#fixture').evaluate((fixture) => { fixture.style.width = '520px'; });

  const combined = page.locator('.task-item').filter({ hasText: 'Minden metaadat' });
  const geometry = await combined.evaluate((item) => {
    const row = item.querySelector('.task-title-row').getBoundingClientRect();
    const optional = item.querySelector('.optional-meta').getBoundingClientRect();
    const trailing = item.querySelector('.trailing-meta').getBoundingClientRect();
    const status = item.querySelector('.status-badge').getBoundingClientRect();
    const priority = item.querySelector('.priority-badge').getBoundingClientRect();
    return {
      rowHeight: row.height,
      rowRight: row.right,
      optionalRight: optional.right,
      trailingLeft: trailing.left,
      statusTop: status.top,
      priorityTop: priority.top,
      priorityRight: priority.right,
    };
  });

  expect(geometry.rowHeight).toBeLessThanOrEqual(24);
  expect(geometry.priorityRight).toBeLessThanOrEqual(geometry.rowRight + 1);
  expect(geometry.optionalRight).toBeLessThanOrEqual(geometry.trailingLeft + 1);
  expect(Math.abs(geometry.statusTop - geometry.priorityTop)).toBeLessThanOrEqual(1);
});

test('TaskPanel keeps trailing metadata inside the realistic 300-320px main panel', async ({ page }) => {
  await gotoTaskPanelFixture(page);
  const combined = page.locator('.task-item').filter({ hasText: 'Minden metaadat' });
  for (const width of [320, 300]) {
    await page.locator('#fixture').evaluate((fixture, value) => { fixture.style.width = `${value}px`; }, width);
    const geometry = await combined.evaluate((item) => {
      const row = item.querySelector('.task-title-row').getBoundingClientRect();
      const status = item.querySelector('.status-badge').getBoundingClientRect();
      const priority = item.querySelector('.priority-badge').getBoundingClientRect();
      return { rowLeft: row.left, rowRight: row.right, statusLeft: status.left, priorityRight: priority.right };
    });
    expect(geometry.statusLeft).toBeGreaterThanOrEqual(geometry.rowLeft - 1);
    expect(geometry.priorityRight).toBeLessThanOrEqual(geometry.rowRight + 1);
  }
});

test('TaskPanel closes stale context menus and edit modals on session change', async ({ page }) => {
  await gotoTaskPanelFixture(page);
  await page.locator('.task-item').first().click({ button: 'right' });
  await expect(page.locator('.context-menu')).toBeVisible();
  await page.evaluate(() => window.taskPanelFixture.select('other-session', [{
    id: '4', title: 'OTHER SESSION ID 4', description: 'keep', details: 'keep', status: 'pending',
    priority: 'low', tags: [], dependencies: [], subtasks: [],
  }]));
  await expect(page.getByText('OTHER SESSION ID 4')).toBeVisible();
  await expect(page.locator('.context-menu')).toHaveCount(0);

  await page.locator('.task-item').first().click({ button: 'right' });
  await page.locator('.context-menu button').filter({ hasText: /Edit|Szerkeszt/ }).click();
  await expect(page.locator('.dialog-overlay')).toBeVisible();
  await page.evaluate(() => window.taskPanelFixture.select('third-session', [{
    id: '4', title: 'THIRD SESSION ID 4', description: 'keep', details: 'keep', status: 'pending',
    priority: 'low', tags: [], dependencies: [], subtasks: [],
  }]));
  await expect(page.locator('.dialog-overlay')).toHaveCount(0);
  expect(await page.evaluate(() => window.taskPanelFixture.updates())).toEqual([]);
});

test('a delayed TaskPanel save cannot close or overwrite a newer session modal', async ({ page }) => {
  await gotoTaskPanelFixture(page, '?delayUpdate=1');
  await page.locator('.task-item').first().click({ button: 'right' });
  await page.locator('.context-menu button').filter({ hasText: /Edit|Szerkeszt/ }).click();
  const firstTitle = page.locator('.dialog-content input[type="text"]').first();
  await firstTitle.fill('delayed session A edit');
  await page.locator('.dialog-content .btn-primary').click();
  await expect.poll(() => page.evaluate(() => window.taskPanelFixture.updates().length)).toBe(1);

  await page.evaluate(() => window.taskPanelFixture.select('new-session', [{
    id: '4', title: 'NEW SESSION TASK', description: '', details: '', status: 'pending',
    priority: 'low', tags: [], dependencies: [], subtasks: [],
  }]));
  await expect(page.getByText('NEW SESSION TASK')).toBeVisible();
  await page.locator('.task-item').first().click({ button: 'right' });
  await page.locator('.context-menu button').filter({ hasText: /Edit|Szerkeszt/ }).click();
  const newTitle = page.locator('.dialog-content input[type="text"]').first();
  await newTitle.fill('new session draft survives');

  await page.evaluate(() => window.taskPanelFixture.resolveUpdates());
  await expect(newTitle).toBeVisible();
  await expect(newTitle).toHaveValue('new session draft survives');
});

test('TaskPanel does not offer AI-only actions after MCP falls back to local tasks', async ({ page }) => {
  await gotoTaskPanelFixture(page, '?fallback=1');
  await expect(page.locator('.task-item')).toHaveCount(4);
  await page.getByRole('button', { name: /Add Task|Feladat hozzáadása/ }).click();
  await expect(page.getByRole('button', { name: /AI Generated|AI által generált/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Expand All|Összes kibontása/ })).toHaveCount(0);
});

test('Notes preserves a per-target draft after save failure and fails closed on load failure', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await gotoNotesFixture(page);

  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('saved A', { timeout: 15000 });
  await textarea.fill('draft A survives');
  await page.evaluate(() => window.notesFixture.select('notes-b'));
  await expect(textarea).toHaveValue('saved B');
  await page.evaluate(() => window.notesFixture.select('notes-a'));

  await expect(textarea).toHaveValue('draft A survives');
  await expect(page.locator('.notes-error')).toContainText('save refused');
  await expect(textarea).toBeEnabled();
  expect(await page.evaluate(() => window.notesFixture.stored('notes-a', 'project-a'))).toBe('saved A');

  await page.evaluate(() => window.notesFixture.select('notes-load-fails'));
  await expect(page.locator('.notes-error')).toContainText('load refused');
  await expect(textarea).toBeDisabled();
  expect(pageErrors).toEqual([]);
});

test('Notes keeps identical session and tab IDs isolated between projects', async ({ page }) => {
  await gotoNotesFixture(page);
  const textarea = page.locator('.notes-textarea');

  await page.evaluate(() => window.notesFixture.select('notes-b'));
  await expect(textarea).toHaveValue('saved B');

  await page.evaluate(() => window.notesFixture.replaceProject('project-b', 'notes-b'));
  await expect(textarea).toHaveValue('saved B in project B');

  await page.evaluate(() => window.notesFixture.replaceProject('project-a', 'notes-b'));
  await expect(textarea).toHaveValue('saved B');
});

test('Notes blocks a destructive action for a failed background draft', async ({ page }) => {
  await gotoNotesFixture(page);
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('saved A', { timeout: 15000 });
  await textarea.fill('draft A survives quit');
  await page.evaluate(() => window.notesFixture.select('notes-b'));
  await expect(textarea).toHaveValue('saved B');
  await expect.poll(() => page.evaluate(() => window.notesFixture.failedSaves())).toBe(1);

  await page.evaluate(() => window.notesFixture.attemptDestructive());
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  expect(await dialog.evaluate((node) => node.parentElement === document.body)).toBe(true);
  await page.getByRole('button', { name: /Keep editing|Szerkesztés folytatása/ }).click();
  await expect(page.locator('body')).toHaveAttribute('data-destructive', 'false');

  await page.evaluate(() => window.notesFixture.attemptDestructive());
  await page.getByRole('button', { name: /Discard changes|Módosítások elvetése/ }).click();
  await expect(page.locator('body')).toHaveAttribute('data-destructive', 'true');
});

test('a destructive action re-confirms a Notes draft changed while another editor prompt is open', async ({ page }) => {
  await gotoNotesFixture(page);
  const textarea = page.locator('.notes-textarea');
  await expect(textarea).toHaveValue('saved A', { timeout: 15000 });

  // Notes is registered first; this second guard keeps the destructive flow
  // pending after the first Notes approval.
  await page.evaluate(() => window.notesFixture.enableSecondGuard());
  await textarea.fill('first draft');
  await page.evaluate(() => window.notesFixture.attemptDestructive());
  await page.getByRole('button', { name: /Discard changes|Módosítások elvetése/ }).click();
  await expect(page.locator('body')).toHaveAttribute('data-second-prompt', 'true');

  // A new draft is a different revision from the one already approved. It
  // must be confirmed again after the second editor releases the action.
  await textarea.fill('new draft while another confirmation is open');
  await page.evaluate(() => window.notesFixture.approveSecondGuard());
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.locator('body')).toHaveAttribute('data-destructive', 'false');

  await page.getByRole('button', { name: /Discard changes|Módosítások elvetése/ }).click();
  await expect(page.locator('body')).toHaveAttribute('data-destructive', 'true');
});

test('SchemeImport ignores a stale source response and traps focus inside its modal', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'scheme');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.localSchemeCalls())).toBe(1);

  // The shared focus action starts on the close button. Reverse tabbing must
  // wrap to the last control in the dialog, never to a fixture/Settings
  // control behind the modal.
  await expect(dialog.locator('.close-btn')).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  expect(await page.evaluate(() => {
    const modal = document.querySelector('[role="dialog"]');
    return !!modal?.contains(document.activeElement);
  })).toBe(true);

  await dialog.locator('.source-tabs button').nth(1).click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.onlineSchemeCalls())).toBe(1);
  await page.evaluate(() => window.dialogRacesFixture.resolveLocalSchemes(0, 'stale local result'));
  await expect(dialog).not.toContainText('stale local result');
  await page.evaluate(() => window.dialogRacesFixture.resolveOnlineSchemes(0, 'current online result'));
  await expect(dialog).toContainText('current online result');
});

test('project import cannot be dismissed and duplicated while its durable operation is pending', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'import');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  await dialog.locator('.select-trigger').click();
  await page.locator('body > .select-dropdown').getByRole('button', { name: 'Source Project' }).click();
  const session = dialog.getByText('Portable Session');
  await expect(session).toBeVisible();
  await dialog.locator('input[type="checkbox"]').check();
  await dialog.getByRole('button', { name: /Import Selected|Kijelöltek importálása/ }).click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.importSessionCalls().length)).toBe(1);

  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('.close-btn')).toBeDisabled();
  await expect(dialog.locator('.btn-cancel')).toBeDisabled();
  await expect(dialog.locator('.btn-primary')).toBeDisabled();
  expect(await page.evaluate(() => window.dialogRacesFixture.importSessionCalls().length)).toBe(1);

  await page.evaluate(() => window.dialogRacesFixture.resolveImportSessions(0, 1));
  await expect(dialog).toContainText(/Successfully imported 1 session/);
  await expect(dialog.locator('.btn-cancel')).toBeEnabled();
  await dialog.locator('.btn-cancel').click();
  await expect(dialog).toHaveCount(0);
});

test('portable file import cannot be dismissed and duplicated while its token is pending', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'sessionfile');
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByText('Portable File Session')).toBeVisible();
  await dialog.getByRole('button', { name: /Import \(1\)|Importálás \(1\)/ }).click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.sessionFileImportCalls().length)).toBe(1);

  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('.close-btn')).toBeDisabled();
  await expect(dialog.locator('.btn-secondary')).toBeDisabled();
  await expect(dialog.locator('.btn-primary')).toBeDisabled();
  expect(await page.evaluate(() => window.dialogRacesFixture.sessionFileImportCalls().length)).toBe(1);

  await page.evaluate(() => window.dialogRacesFixture.resolveSessionFileImport(0, 1));
  await expect(dialog).toContainText(/1 sessions imported|1 munkamenet importálva/);
  await dialog.locator('.btn-primary').click();
  await expect(dialog).toHaveCount(0);
});

test('AllTasks detail owns focus, closes with Escape and restores its opener', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'alltasks');
  const opener = page.getByRole('button', { name: /Fixture dashboard task/ });
  await expect(opener).toBeVisible();
  await opener.focus();
  await opener.press('Enter');

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: /Close|Bezárás/ })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
});

test('project switching is cancelled or delayed until a Notes draft is discarded', async ({ page }) => {
  await gotoNotesFixture(page);
  const textarea = page.locator('textarea');
  await expect(textarea).toHaveValue('saved A');
  await textarea.fill('project-scoped draft');

  await page.evaluate(() => window.notesFixture.switchProject('project-b'));
  let dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: /Keep editing|Szerkesztés folytatása/ }).click();
  expect(await page.evaluate(() => window.notesFixture.selectedProject())).toBe('project-a');
  await expect(textarea).toHaveValue('project-scoped draft');

  await page.evaluate(() => window.notesFixture.switchProject('project-b'));
  dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: /Discard changes|Módosítások elvetése/ }).click();
  await expect.poll(() => page.evaluate(() => window.notesFixture.selectedProject())).toBe('project-b');
  expect(await page.evaluate(() => window.notesFixture.stored('notes-a', 'project-a'))).toBe('saved A');
});

test('sidebar session deletion requires confirmation and waits for unsaved editors', async ({ page }) => {
  await page.goto('/tests/browser/session-delete-fixture.html');
  await page.locator('.session-item').click({ button: 'right' });
  await page.getByRole('button', { name: 'Delete', exact: true }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('unfinished fixture task');
  expect(await page.evaluate(() => window.sessionDeleteFixture.deleted())).toEqual([]);

  await dialog.getByRole('button', { name: /^Delete$/ }).click();
  await expect.poll(() => page.evaluate(() => window.sessionDeleteFixture.hasPendingDiscard())).toBe(true);
  expect(await page.evaluate(() => window.sessionDeleteFixture.deleted())).toEqual([]);

  await page.evaluate(() => window.sessionDeleteFixture.approveDiscard());
  await expect.poll(() => page.evaluate(() => window.sessionDeleteFixture.deleted())).toEqual(['delete-target']);
});

test('a session delete lookup cannot open or delete in a replacement project', async ({ page }) => {
  await page.goto('/tests/browser/session-delete-fixture.html');
  await page.evaluate(() => window.sessionDeleteFixture.deferTaskLookup());
  await page.locator('.session-item').click({ button: 'right' });
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.sessionDeleteFixture.taskLookupPending())).toBe(true);

  await page.evaluate(() => window.sessionDeleteFixture.switchProject('project-b'));
  await page.evaluate(() => window.sessionDeleteFixture.resolveTaskLookup());
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.sessionDeleteFixture.deleted())).toEqual([]);
});

test('group bulk stop aborts before a same-id replacement project session', async ({ page }) => {
  await gotoGroupProjectFixture(page);
  await page.locator('.group-header').click({ button: 'right' });
  await page.getByRole('button', { name: /Stop all|Összes leállítása/ }).click();
  await expect.poll(() => page.evaluate(() => window.groupProjectFixture.stopCalls())).toEqual(['session-1']);

  await page.evaluate(() => window.groupProjectFixture.switchProject('project-b'));
  await page.evaluate(() => window.groupProjectFixture.resolveFirstStop());
  await page.waitForTimeout(50);
  expect(await page.evaluate(() => window.groupProjectFixture.stopCalls())).toEqual(['session-1']);

  await page.locator('.group-header').click({ button: 'right' });
  await expect(page.locator('.context-menu')).toBeVisible();
  await page.evaluate(() => window.groupProjectFixture.switchProject('project-c'));
  await expect(page.locator('.context-menu')).toHaveCount(0);
});

test('group bulk actions cannot be started twice while the first session is pending', async ({ page }) => {
  await gotoGroupProjectFixture(page);
  await page.locator('.group-header').click({ button: 'right' });
  await page.getByRole('button', { name: /Stop all|Összes leállítása/ }).click();
  await expect.poll(() => page.evaluate(() => window.groupProjectFixture.stopCalls())).toEqual(['session-1']);

  await page.locator('.group-header').click({ button: 'right' });
  const menu = page.locator('.context-menu');
  await expect(menu.getByRole('button', { name: /Start all|Összes indítása/ })).toBeDisabled();
  await expect(menu.getByRole('button', { name: /Stop all|Összes leállítása/ })).toBeDisabled();
  await page.evaluate(() => window.groupProjectFixture.resolveFirstStop());
  await expect.poll(() => page.evaluate(() => window.groupProjectFixture.stopCalls())).toEqual(['session-1', 'session-2']);
});

test('group bulk actions report partial backend failure after continuing the batch', async ({ page }) => {
  await gotoGroupProjectFixture(page);
  await page.evaluate(() => window.groupProjectFixture.rejectSecondStop());
  await page.locator('.group-header').click({ button: 'right' });
  await page.getByRole('button', { name: /Stop all|Összes leállítása/ }).click();
  await expect.poll(() => page.evaluate(() => window.groupProjectFixture.stopCalls())).toEqual(['session-1']);
  await page.evaluate(() => window.groupProjectFixture.resolveFirstStop());

  await expect.poll(() => page.evaluate(() => window.groupProjectFixture.stopCalls())).toEqual(['session-1', 'session-2']);
  await expect(page.getByRole('alert')).toContainText('Second: Error: fixture stop refused');
});

test('a native session drag payload cannot mutate a replacement project', async ({ page }) => {
  await gotoGroupProjectFixture(page);
  await page.evaluate(() => window.groupProjectFixture.staleSessionDrop());
  expect(await page.evaluate(() => window.groupProjectFixture.assignCalls())).toEqual([]);
});

test('new-session creation cannot continue its start step after project replacement', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'newsession');
  await page.locator('#path').fill('/repo-a/new-session');
  await page.locator('#name').fill('created in A');
  await page.getByRole('button', { name: /Create Session|Munkamenet létrehozása/ }).click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.createSessionPending())).toBe(true);

  await page.evaluate(() => window.dialogRacesFixture.switchRecoveryProject('project-b'));
  await page.evaluate(() => window.dialogRacesFixture.resolveCreateSession());
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.dialogRacesFixture.startSessionCalls())).toEqual([]);
});

test('project-scoped creation and attach dialogs close instead of rebinding their draft', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'newgroup');
  await page.locator('#group-name').fill('Project A draft');
  await page.evaluate(() => window.dialogRacesFixture.switchProject('project-b'));
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.dialogRacesFixture.createGroupCalls())).toEqual([]);

  await gotoDialogRacesFixture(page, 'bgagents');
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.evaluate(() => window.dialogRacesFixture.switchProject('project-b'));
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('a fork draft closes when its project identity is replaced', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'fork');
  await page.locator('#fork-name').fill('Project A branch');
  await page.evaluate(() => window.dialogRacesFixture.switchProject('project-b'));
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.dialogRacesFixture.forkCalls())).toEqual([]);
});

test('command dialogs and template manager trap keyboard focus inside their forms', async ({ page }) => {
  for (const mode of ['commandmanager', 'template', 'command', 'palette']) {
    await gotoDialogRacesFixture(page, mode);
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    const controls = dialog.locator('button:not([disabled]), input:not([disabled]), textarea:not([disabled])');
    const last = controls.last();
    await last.focus();
    await page.keyboard.press('Tab');
    await expect.poll(() => dialog.evaluate((node) => node.contains(document.activeElement))).toBe(true);
  }
});

test('clearing GlobalSearch invalidates an in-flight result and clears its spinner', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'global');
  const input = page.locator('.search-input');
  await input.fill('old query');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.searchCalls().length)).toBe(1);
  await page.locator('.clear-btn').click();
  await page.evaluate(() => window.dialogRacesFixture.resolveSearch([{
    id: 'old', agent: 'codex', content: 'stale result', sessionId: 'old', score: 1,
  }]));
  await expect(input).toHaveValue('');
  await expect(page.getByText('stale result')).toHaveCount(0);
  await expect(page.locator('.loading-state')).toHaveCount(0);
});

test('CommandPicker snapshots its target and suppresses duplicate execution', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'command');
  const row = page.locator('.cmd-row').filter({ hasText: 'Fixture command' });
  await expect(row).toBeVisible();
  await row.dblclick();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.runCalls().length)).toBe(1);
  expect(await page.evaluate(() => window.dialogRacesFixture.runCalls()[0].slice(0, 3)))
    .toEqual(['command-1', 'session-a', 3]);
  await page.evaluate(() => window.dialogRacesFixture.resolveRun());
});

test('CommandPicker closes when its captured session or tab changes', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'command');
  await expect(page.locator('.cmd-row').filter({ hasText: 'Fixture command' })).toBeVisible();
  await page.locator('#change-command-target').click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.dialogRacesFixture.runCalls())).toEqual([]);
});

test('QuickJump ignores a late response from an earlier open cycle', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'quickjump');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.quickJumpCalls())).toBe(1);

  await page.getByRole('button', { name: /Close|Bezárás/ }).click();
  await page.locator('#reopen-quickjump').evaluate((button) => button.click());
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.quickJumpCalls())).toBe(2);

  await page.evaluate(() => window.dialogRacesFixture.resolveQuickJump(1, 'new-session', 'new list'));
  await expect(page.getByText('new list')).toBeVisible();
  await page.evaluate(() => window.dialogRacesFixture.resolveQuickJump(0, 'old-session', 'stale list'));
  await expect(page.getByText('new list')).toBeVisible();
  await expect(page.getByText('stale list')).toHaveCount(0);
});

test('QuickJump owns focus and Escape while its initial backend read is pending', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'quickjump');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect.poll(() => dialog.evaluate((node) => node.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
});

test('QuickTerminal submits once and an old completion cannot close its replacement', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'quickterminal');
  const input = page.getByRole('dialog').locator('input');
  await expect(input).toBeFocused();
  await input.fill('first terminal');
  await input.press('Enter');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.createTabCalls().length)).toBe(1);

  await page.getByRole('button', { name: /Cancel|Mégse/ }).click();
  await page.locator('#reopen-quickterminal').click();
  const replacement = page.getByRole('dialog').locator('input');
  await replacement.fill('replacement draft');
  await page.evaluate(() => window.dialogRacesFixture.resolveCreateTab(0, 4));

  await expect(replacement).toBeVisible();
  await expect(replacement).toHaveValue('replacement draft');
  expect(await page.evaluate(() => window.dialogRacesFixture.createTabCalls().length)).toBe(1);

  const create = page.getByRole('button', { name: /Create|Létrehozás/ });
  await create.focus();
  await page.keyboard.press('Tab');
  await expect(replacement).toBeFocused();
});

test('GitHistory ignores a late response from the repository that was left', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'history');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.historyCalls())).toContain('/repo-a');
  await page.locator('#change-history-target').click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.historyCalls())).toContain('/repo-b');
  await page.evaluate(() => window.dialogRacesFixture.resolveHistory('/repo-b', 'NEW REPOSITORY COMMIT'));
  await expect(page.locator('.commit-subject').filter({ hasText: 'NEW REPOSITORY COMMIT' })).toBeVisible();
  await page.evaluate(() => window.dialogRacesFixture.resolveHistory('/repo-a', 'STALE REPOSITORY COMMIT'));
  await expect(page.locator('.commit-subject').filter({ hasText: 'NEW REPOSITORY COMMIT' })).toBeVisible();
  await expect(page.locator('.commit-subject').filter({ hasText: 'STALE REPOSITORY COMMIT' })).toHaveCount(0);
});

test('Recovery ignores an old-project restore completion after the project changes', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'recovery');
  const restore = page.getByRole('button', { name: /Restore|Visszaállítás/ }).first();
  await expect(restore).toBeVisible();
  await restore.click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.trashRestorePending())).toBe(true);

  await page.evaluate(() => window.dialogRacesFixture.switchRecoveryProject('project-b'));
  await page.evaluate(() => window.dialogRacesFixture.resolveTrashRestore('old-project-session'));

  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.recoverySessionLoads())).toBe(0);
  expect(await page.evaluate(() => window.dialogRacesFixture.selectedSession())).toBe(null);
});

test('Update cannot be dismissed through any close path while installation is running', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'update');
  const dialog = page.getByRole('dialog');
  const update = dialog.getByRole('button', { name: /Update Now|Frissítés most/i });
  await expect(update).toBeVisible();
  await update.click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.updateCalls())).toBe(1);

  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('.close-btn')).toBeDisabled();
  await expect(dialog.locator('.dialog-footer .btn-secondary')).toBeDisabled();
  await page.locator('.dialog-overlay').dispatchEvent('click');
  await expect(dialog).toBeVisible();

  await page.evaluate(() => window.dialogRacesFixture.resolveUpdate());
  await expect(dialog.locator('.dialog-footer .btn-secondary')).toBeEnabled();
});

test('Update offers a manual release path without calling PerformUpdate when auto-install is unsupported', async ({ page }) => {
  await page.goto('/tests/browser/dialog-races-fixture.html?mode=update&manualUpdate=1');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('complete portable EXE and DLL set');
  await expect(dialog.getByRole('button', { name: /Update Now|Frissítés most/i })).toHaveCount(0);
  await dialog.getByRole('button', { name: /Open download page|Letöltési oldal megnyitása/i }).click();
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.openedURLs())).toEqual([
    'https://github.com/izll/agent-session-manager-desktop/releases/latest',
  ]);
  expect(await page.evaluate(() => window.dialogRacesFixture.updateCalls())).toBe(0);
});

test('Settings persists a focused ntfy address before Escape removes the input', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'settings');
  const input = page.locator('input[placeholder="https://ntfy.sh/my-topic"]');
  await expect(input).toBeVisible();
  await input.fill('https://ntfy.sh/new-topic');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => {
    const saves = window.dialogRacesFixture.settingsSaves();
    return saves.at(-1)?.ntfyUrl;
  })).toBe('https://ntfy.sh/new-topic');
});

test('dashboard usage polling is single-flight per provider', async ({ page }) => {
  await page.clock.install();
  await gotoDialogRacesFixture(page, 'dashboard');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.claudeUsageCalls())).toBe(1);
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.codexUsageCalls())).toBe(1);

  await page.clock.runFor(45_000);
  expect(await page.evaluate(() => window.dialogRacesFixture.claudeUsageCalls())).toBe(1);
  expect(await page.evaluate(() => window.dialogRacesFixture.codexUsageCalls())).toBe(4);

  await page.evaluate(() => window.dialogRacesFixture.resolveClaudeUsage(0));
  await page.clock.runFor(15_000);
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.claudeUsageCalls())).toBe(2);
});

test('All Tasks coalesces refresh bursts into one follow-up scan', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'alltasks&delayTasks=1');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(1);

  await page.evaluate(() => {
    window.dispatchEvent(new Event('tasks:refresh'));
    window.dispatchEvent(new Event('tasks:refresh'));
    window.dispatchEvent(new Event('tasks:refresh'));
  });
  expect(await page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(1);

  await page.evaluate(() => window.dialogRacesFixture.resolveAllTasks(0));
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(2);
  await page.evaluate(() => window.dialogRacesFixture.resolveAllTasks(1));
  await expect(page.getByText('Fixture dashboard task')).toBeVisible();
});

test('the open-task badge coalesces refresh bursts while its project scan is pending', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'taskbadge&delayTasks=1');
  await page.evaluate(() => {
    window.dialogRacesFixture.triggerOpenTaskRefresh();
    window.dialogRacesFixture.triggerOpenTaskRefresh();
    window.dialogRacesFixture.triggerOpenTaskRefresh();
  });
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(1);

  await page.evaluate(() => window.dialogRacesFixture.resolveAllTasks(0));
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(2);
  await page.evaluate(() => window.dialogRacesFixture.resolveAllTasks(1));
  await page.waitForTimeout(0);
  expect(await page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(2);

  // Resolve first, then queue a request behind the async drain continuation
  // but ahead of its Promise.finally callback. That narrow ordering used to
  // leave refreshQueued=true with no active drain to consume it.
  await page.evaluate(() => window.dialogRacesFixture.triggerOpenTaskRefresh());
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(3);
  await page.evaluate(() => {
    window.dialogRacesFixture.resolveAllTasks(2);
    queueMicrotask(() => window.dialogRacesFixture.triggerOpenTaskRefresh());
  });
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.allTaskCalls())).toBe(4);
  await page.evaluate(() => window.dialogRacesFixture.resolveAllTasks(3));
});

test('background-agent polling stays single-flight while a backend read is pending', async ({ page }) => {
  await page.clock.install();
  await gotoDialogRacesFixture(page, 'bgagents&delayBgAgents=1');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.bgAgentCalls())).toBe(1);

  await page.clock.runFor(20_000);
  expect(await page.evaluate(() => window.dialogRacesFixture.bgAgentCalls())).toBe(1);
  await page.evaluate(() => window.dialogRacesFixture.resolveBgAgents(0));
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.bgAgentCalls())).toBe(2);
  await page.evaluate(() => window.dialogRacesFixture.resolveBgAgents(1));
  await expect(page.getByText('Background fixture')).toBeVisible();
});

test('closing GitHistory cancels an active document drag listener', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'history');
  const dialog = page.locator('.history-dialog');
  const initialWidth = (await dialog.boundingBox()).width;
  const handle = await page.locator('.dialog-resizer').boundingBox();
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
  await page.mouse.down();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await page.mouse.move(handle.x + 120, handle.y + 80);
  await page.mouse.up();

  await page.locator('#reopen-history').click();
  await expect(dialog).toBeVisible();
  const reopenedWidth = (await dialog.boundingBox()).width;
  expect(Math.abs(reopenedWidth - initialWidth)).toBeLessThanOrEqual(2);
});

test('project-aware FileBrowser and Diff guards accept their current file responses', async ({ page }) => {
  await page.goto('/tests/browser/project-content-fixture.html');
  // This fixture is the first browser entry that compiles both CodeMirror's
  // language graph and the full diff renderer. A measured cold transform is
  // ~17 s on the test host; wait for its explicit post-mount signal rather
  // than treating compilation as a missing component.
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });

  const browser = page.locator('#browser');
  await browser.locator('.file-row', { hasText: 'browser.txt' }).click();
  await expect.poll(() => page.evaluate(() => window.projectContentFixture.browserReads())).toBe(1);
  await expect(browser.locator('.cm-content')).toContainText('browser content marker');

  const diff = page.locator('#diff');
  await expect.poll(() => page.evaluate(() => window.projectContentFixture.diffReads())).toBe(1);
  await expect(diff.locator('.revert-error')).toContainText('fixture diff read refused');
  await diff.locator('.refresh-btn').click();
  await expect.poll(() => page.evaluate(() => window.projectContentFixture.diffReads())).toBe(2);
  await expect(diff.locator('.diff-lines')).toContainText('diff content marker');

  // A pane width saved on a desktop-sized window must not leave the document
  // or diff as an unusable sliver after narrowing/zooming the main panel.
  // Before the responsive cap both right-hand panes measured 18 px here.
  const narrowWidths = await page.evaluate(() => {
    const measurements = {};
    for (const id of ['browser', 'diff']) {
      const root = document.getElementById(id);
      root.style.width = '300px';
      const content = root.querySelector(id === 'browser' ? '.file-content' : '.diff-content');
      measurements[id] = content.getBoundingClientRect().width;
    }
    return measurements;
  });
  expect(narrowWidths.browser).toBeGreaterThanOrEqual(115);
  expect(narrowWidths.diff).toBeGreaterThanOrEqual(115);
});
