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
  expect(await page.evaluate(() => window.notesFixture.stored('notes-a'))).toBe('saved A');

  await page.evaluate(() => window.notesFixture.select('notes-load-fails'));
  await expect(page.locator('.notes-error')).toContainText('load refused');
  await expect(textarea).toBeDisabled();
  expect(pageErrors).toEqual([]);
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
  expect(await page.evaluate(() => window.notesFixture.selectedProject())).toBe('');
  await expect(textarea).toHaveValue('project-scoped draft');

  await page.evaluate(() => window.notesFixture.switchProject('project-b'));
  dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: /Discard changes|Módosítások elvetése/ }).click();
  await expect.poll(() => page.evaluate(() => window.notesFixture.selectedProject())).toBe('project-b');
  expect(await page.evaluate(() => window.notesFixture.stored('notes-a'))).toBe('saved A');
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

test('clearing GlobalSearch invalidates an in-flight result and clears its spinner', async ({ page }) => {
  await gotoDialogRacesFixture(page, 'global');
  const input = page.locator('.search-input');
  await input.fill('old query');
  await expect.poll(() => page.evaluate(() => window.dialogRacesFixture.searchCalls().length)).toBe(1);
  await page.locator('.clear-btn').click();
  await page.evaluate(() => window.dialogRacesFixture.resolveSearch([{
    agent: 'codex', content: 'stale result', sessionFile: 'old', sessionId: 'old', score: 1,
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
