import { test, expect } from '@playwright/test';

// The project's own task list and note: opened from the header button, worked
// on in its window, visible in the all-tasks view, and handed to a session.

async function gotoFixture(page) {
  await page.goto('/tests/browser/project-tasks-fixture.html');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
}

const calls = (page) => page.evaluate(() => window.projectTasksFixture.calls());
const dialog = (page) => page.locator('.project-tasks-dialog');
const projectRows = (page) => dialog(page).locator('.task-item');

async function openWindow(page) {
  await page.getByRole('button', { name: /^Project tasks/ }).click();
  await expect(dialog(page)).toBeVisible();
  await expect(projectRows(page)).toHaveCount(2);
}

test('the header button counts open project tasks and opens their window', async ({ page }) => {
  await gotoFixture(page);
  const button = page.getByRole('button', { name: /^Project tasks/ });
  // The shortcut is named where the button is, and follows a rebinding.
  await expect(button).toHaveAttribute('title', 'Project tasks (Ctrl+Shift+B)');
  await expect(button.locator('.count-badge')).toHaveText('2');

  await button.click();
  await expect(dialog(page).locator('.dialog-header h2')).toHaveText(/^Project tasks — /);
  await expect(projectRows(page)).toHaveCount(2);
  await expect(dialog(page)).toContainText('Plan the release');

  await page.keyboard.press('Escape');
  await expect(dialog(page)).toHaveCount(0);
});

test('a task added in the window lands in the project list, tied to no session', async ({ page }) => {
  await gotoFixture(page);
  await openWindow(page);

  await dialog(page).getByRole('button', { name: '+ Add Task' }).click();
  const add = page.locator('.dialog-content.large').last();
  await add.locator('input[type="text"]').first().fill('Update the roadmap');
  await add.getByRole('button', { name: 'Add Task', exact: true }).click();

  await expect(projectRows(page)).toHaveCount(3);
  await expect(dialog(page)).toContainText('Update the roadmap');
  const created = (await calls(page)).find((call) => call[0] === 'CreateTask');
  expect(created).toEqual(['CreateTask', '@project', 'Update the roadmap', '']);
  // No tab was sent: the project list has none to give.
  expect((await calls(page)).some((call) => call[0] === 'UpdateTask')).toBe(false);
  await expect(page.getByRole('button', { name: /^Project tasks/ }).locator('.count-badge')).toHaveText('3');
});

test('the all-tasks view shows project tasks as their own group, and follows a move into a session', async ({ page }) => {
  await gotoFixture(page);
  const overview = page.locator('.fixture-all-tasks');
  const projectGroup = overview.locator('.group-head', { hasText: 'project tasks' });
  await expect(projectGroup).toHaveCount(1);
  await expect(overview).toContainText('Plan the release');

  await openWindow(page);
  await projectRows(page).filter({ hasText: 'Plan the release' }).click({ button: 'right' });
  await page.locator('.context-menu').getByRole('button', { name: 'Move to session…' }).click();

  const picker = page.locator('.session-pick-dialog');
  await expect(picker.locator('.pick-task')).toHaveText('Plan the release');
  // The session being looked at is offered first; pick its Codex tab.
  const selects = picker.locator('.select-trigger');
  await expect(selects.nth(0)).toHaveText('API server');
  await expect(selects.nth(1)).toHaveText('Any tab');
  await selects.nth(1).click();
  await page.locator('.select-dropdown .select-option', { hasText: 'Codex' }).click();
  await picker.getByRole('button', { name: 'Move', exact: true }).click();
  await expect(picker).toHaveCount(0);

  await expect(projectRows(page)).toHaveCount(1);
  const move = (await calls(page)).find((call) => call[0] === 'MoveTaskToSession');
  expect(move).toEqual(['MoveTaskToSession', 'p1', 'session-a', 'tab-codex', '']);
  expect(await page.evaluate(() => window.projectTasksFixture.list('session-a').map((t) => t.title)))
    .toContain('Plan the release');

  // The overview, open behind the window, moved it under the session.
  const sessionGroupTitles = overview.locator('.group-head', { hasText: 'API server' });
  await expect(sessionGroupTitles).toHaveCount(1);
  await page.keyboard.press('Escape');
  await expect(overview.locator('.task-row', { hasText: 'Plan the release' })).toHaveCount(1);
  await expect(page.getByRole('button', { name: /^Project tasks/ }).locator('.count-badge')).toHaveText('1');
});

test('sending a project task asks for the session and leaves the task where it is', async ({ page }) => {
  await gotoFixture(page);
  await openWindow(page);
  await projectRows(page).filter({ hasText: 'Write the changelog' }).click({ button: 'right' });
  await page.locator('.context-menu').getByRole('button', { name: 'Send to agent…' }).click();

  const picker = page.locator('.session-pick-dialog');
  await picker.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(picker).toHaveCount(0);
  const sent = (await calls(page)).find((call) => call[0] === 'SendProjectTaskToAgent');
  expect(sent).toEqual(['SendProjectTaskToAgent', 'p2', 'session-a', '', '']);
  // The window followed the task to its agent; the list still has it.
  await expect(dialog(page)).toHaveCount(0);
  expect(await page.evaluate(() => window.projectTasksFixture.list('@project').map((t) => t.id))).toContain('p2');
});

test('the project note loads and saves, in the default project too', async ({ page }) => {
  await gotoFixture(page);
  await openWindow(page);
  await dialog(page).getByRole('tab', { name: 'Notes' }).click();
  const note = dialog(page).locator('textarea.notes-textarea');
  await expect(note).toHaveValue('Kickoff on Monday');
  await expect(dialog(page).locator('.notes-title')).toHaveText('Project notes');

  await note.fill('Kickoff on Monday\nRelease on Friday');
  // Autosaved — with the default project's own ID, which is the empty string.
  await expect.poll(() => page.evaluate(() => window.projectTasksFixture.notes()))
    .toBe('Kickoff on Monday\nRelease on Friday');
  const saved = (await calls(page)).filter((call) => call[0] === 'SetProjectNotePages').at(-1);
  expect(saved).toEqual(['SetProjectNotePages', [{ id: 'page-1', title: '', text: 'Kickoff on Monday\nRelease on Friday' }], '']);
  await expect(dialog(page).locator('.save-indicator.unsaved')).toHaveCount(0);
});

test('the project note has pages of its own', async ({ page }) => {
  await gotoFixture(page);
  await openWindow(page);
  await dialog(page).getByRole('tab', { name: 'Notes' }).click();
  const note = dialog(page).locator('textarea.notes-textarea');
  await expect(note).toHaveValue('Kickoff on Monday');

  await dialog(page).getByRole('button', { name: 'New page' }).click();
  const title = dialog(page).locator('.page-title-input');
  await expect(title).toBeFocused();
  await title.fill('Budget');
  await title.press('Enter');
  await expect(note).toBeFocused();
  await expect(note).toHaveValue('');
  await note.fill('Within limits');

  await expect.poll(() => page.evaluate(() => window.projectTasksFixture.notePages().map((p) => [p.title, p.text])))
    .toEqual([['', 'Kickoff on Monday'], ['Budget', 'Within limits']]);
  await dialog(page).locator('.page-tab', { hasText: 'Note 1' }).click();
  await expect(note).toHaveValue('Kickoff on Monday');
});

// The global search finds the project's own note, and opening a result opens
// the project window on the page of the match with the match selected — even
// with the dashboard's copy of the note on screen behind it.
test('a project note found by the global search opens on its page in the project window', async ({ page }) => {
  await page.goto('/tests/browser/project-tasks-fixture.html?dashboard');
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  await page.evaluate(() => window.projectTasksFixture.setNotePages([
    { id: 'p-plan', title: 'Plan', text: 'Kickoff on Monday' },
    { id: 'p-launch', title: 'Launch', text: 'first line\nthe release train leaves Friday' },
  ]));

  await page.evaluate(() => window.openGlobalSearch());
  await page.locator('.search-input').fill('train');
  const result = page.locator('.note-result');
  await expect(result).toHaveCount(1);
  await expect(result.locator('.note-badge')).toHaveText('Project note');
  await expect(result.locator('.note-place')).toHaveText('Default · Launch');
  await result.dblclick();

  await expect(page.locator('.search-input')).toHaveCount(0);
  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page).getByRole('tab', { name: 'Notes' })).toHaveAttribute('aria-selected', 'true');
  const note = dialog(page).locator('textarea.notes-textarea');
  await expect(dialog(page).locator('.page-tab', { hasText: 'Launch' })).toHaveAttribute('aria-selected', 'true');
  await expect(note).toHaveValue('first line\nthe release train leaves Friday');
  await expect(dialog(page).locator('.find-bar input')).toHaveValue('train');
  await expect.poll(() => note.evaluate((el) => el.value.slice(el.selectionStart, el.selectionEnd))).toBe('train');
  // The dashboard's copy behind the window was left alone.
  await expect(page.locator('.fixture-dashboard .find-bar')).toHaveCount(0);
});

// The project note lives in the project tasks window. Switching to it there
// hands it the keyboard, as opening the session notes does.
test('switching to the project note in its window takes the keyboard', async ({ page }) => {
  await gotoFixture(page);
  await openWindow(page);
  await dialog(page).getByRole('tab', { name: 'Notes' }).click();
  const note = dialog(page).locator('textarea.notes-textarea');
  await expect(note).toBeEnabled();
  await expect(note).toBeFocused();
});
