import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');
const browser = read('lib/components/MainPanel/FileBrowser.svelte');
const diff = read('lib/components/MainPanel/Diff.svelte');
const notes = read('lib/components/MainPanel/Notes.svelte');
const tasks = read('lib/stores/tasks.ts');
const taskPanel = read('lib/components/MainPanel/TaskPanel.svelte');
const undo = read('lib/stores/undo.ts');
const undoToast = read('lib/components/common/UndoToast.svelte');

// A target reset must be held behind the same unsaved-change guard as a file
// click. resetForSession itself must not erase the action that shows the prompt.
const reset = browser.match(/function resetForSession\(\)[\s\S]*?\n  \}/)?.[0] ?? '';
assert.doesNotMatch(reset, /pendingLeave = null/, 'session reset must not erase its own discard prompt');
assert.match(browser, /if \(modified\) \{[\s\S]*?applyBrowseTarget/, 'dirty target changes must be deferred');

// The browser owns one tree and one location per session+tab, not per session.
assert.match(browser, /const lastFileByTab = new Map/);
assert.match(browser, /return `\$\{loadedBrowseKey\}\|\$\{path \|\| ''\}`/);
assert.match(browser, /let openedRoot = ''/);
assert.match(browser, /App\.SaveSessionFileEdit\([\s\S]*?openedRoot/,
  'file saves must carry the absolute root whose editable snapshot was opened');

// A diff jump is consumed only after the target tree has initialized. Source
// order matters for independent Svelte reactive blocks, so assert that too.
const initAt = browser.indexOf('requestBrowseTarget(browseKey');
const jumpAt = browser.indexOf('loadedBrowseKey === browseKey && $pendingFileJump');
assert.ok(initAt >= 0 && jumpAt > initAt, 'target initialization must precede pending jump handling');

// Every diff identity includes the window and mode, including the per-file
// cache; a mode change explicitly drops local contents.
assert.match(diff, /requestedKey = `\$\{sessionId \|\| ''\}:\$\{windowIdx\}:\$\{mode\}`/);
assert.match(diff, /currentDiffKey = `\$\{\$selectedSessionId \|\| ''\}:\$\{\$selectedWindowIdx \?\? 0\}:\$\{diffMode\}`/);
assert.match(diff, /cacheKey = selectedPath[\s\S]*?`\$\{currentDiffKey\}:\$\{loadedRoot\}:/);
assert.match(diff, /function handleModeChange[\s\S]*?fileCache = \{\}/);
assert.match(diff, /windowIdx !== tabIdx\(\)/, 'late file/list responses must be target-checked');
assert.match(diff, /App\.GetSessionDiffFileList\(sessionId, windowIdx, root\)/);
assert.match(diff, /App\.GetFullDiffForFile\(sessionId, path, whole, windowIdx, expectedRoot\)/);
assert.match(browser, /App\.OpenSessionFileForEdit\(sessionId, selectedPath, get\(selectedWindowIdx\) \?\? 0, expectedRoot\)/);
assert.match(browser, /if \(!file\.root\)/, 'an edit snapshot without a canonical root must fail closed');

// Reactivation first flushes pending text and refuses to reload if another
// keystroke made the textarea dirty during that flush. Per-target queues keep
// overlapping writes in order.
assert.match(notes, /await flushPendingSave\(\)/);
assert.match(notes, /if \(saveTimeout \|\| notes !== lastSaved\) return/);
assert.match(notes, /const saveQueues = new Map/);
assert.match(notes, /const previous = saveQueues\.get\(key\) \?\? Promise\.resolve\(\)/);
assert.match(
  notes,
  /const pendingSave = saveQueues\.get\(targetKey\);[\s\S]*?if \(pendingSave\) await pendingSave;[\s\S]*?App\.GetTabNotes/,
  'returning to a tab must wait for its previous queued save before reading',
);
assert.match(notes, /loadingNotes = true/);
assert.match(notes, /<textarea[\s\S]*?disabled=\{loadingNotes \|\| !!loadError\}/, 'an incoming or failed load must not expose an editable unknown document');
assert.match(notes, /if \(loadingNotes \|\| loadError\) return;[\s\S]*?recordHistory\(\)/, 'a synthetic input during loading must not queue a cross-tab save');
assert.match(notes, /const draftsByTarget = new Map/);
assert.match(notes, /latestDraft\.saveError \|\| latestDraft\.text !== latestDraft\.saved/,
  'a failed or dirty per-target draft must win over a stale backend read');

// Once MCP loading falls back, mutations use the provider that actually
// produced the visible list. Late mutations cannot edit another session's
// global store.
assert.match(tasks, /effectiveProviderBySession/);
assert.match(tasks, /export function prepareTasksSession[\s\S]*?tasks\.set\(\[\]\)/);
assert.ok(
  taskPanel.indexOf('prepareTasksSession(sessionId)') < taskPanel.indexOf('await checkTaskMasterStatus(sessionId)'),
  'the previous task list must be cleared before the provider probe awaits',
);
assert.match(tasks, /rememberProvider\(sessionId, requestedMCP, 'local'\)/);
assert.match(tasks, /if \(providerFor\(sessionId\) === 'mcp'\)/);
const removeTask = tasks.match(/export async function removeTask[\s\S]*?\n\}/)?.[0] ?? '';
assert.match(removeTask, /isActiveTasksSession\(sessionId\)/);
assert.match(tasks, /async function reloadTasksIfActive/);
assert.match(tasks, /const localMutationQueues = new Map/);
const localEdit = tasks.match(/async function editTaskLocally[\s\S]*?\n\}/)?.[0] ?? '';
assert.match(localEdit, /const previous = localMutationQueues\.get\(sessionId\)/);
assert.match(localEdit, /await App\.GetTasks\(sessionId\)/,
  'each queued local mutation must read a fresh backend snapshot');

// Undo failures remain visible and retryable, and deletion undo uses the full
// task snapshot plus the subtask description instead of a title-only rebuild.
assert.match(undo, /error: String\(e\)/);
assert.match(undo, /action: pending, remaining: WINDOW_SECONDS/);
assert.match(undoToast, /\$undoState\.error/);
assert.match(taskPanel, /restoreDeletedTask\(sessionId, removed, provider\)/);
assert.match(taskPanel, /restoreDeletedSubtask\(sessionId, parentId, removed, provider\)/);
assert.match(tasks, /export async function restoreDeletedTask/);
const restoreTask = tasks.match(/export async function restoreDeletedTask[\s\S]*?\n\}/)?.[0] ?? '';
assert.match(restoreTask, /App\.RestoreDeletedTask\(sessionId, provider, new main\.DeletedTaskSnapshot\(snapshot\)\)/);
assert.doesNotMatch(restoreTask, /CreateTask|TaskMasterAddManualTask|TaskMasterRemoveTask/, 'restore must be one atomic backend operation');

const directEdit = tasks.match(/export async function updateTaskDirect[\s\S]*?\n\}/)?.[0] ?? '';
const mcpDirectEdit = directEdit.match(/if \(\(requestedProvider \?\? providerFor\(sessionId\)\) === 'mcp'\) \{[\s\S]*?\} else/)?.[0] ?? '';
assert.match(mcpDirectEdit, /TaskMasterUpdateTaskDirect\([\s\S]*?dueAt/);
assert.doesNotMatch(mcpDirectEdit, /App\.UpdateTask/,
  'an MCP edit must not partially update Task Master and then write the local provider');
assert.match(taskPanel, /\$settings\.taskMasterEnabled !== loadedTaskMasterSetting[\s\S]*?loadTasksIfNeeded\(true\)/,
  'changing the provider setting while the panel stays active must invalidate and reload the list');
assert.match(taskPanel, /type TaskActionTarget = \{[\s\S]*?provider: TaskProvider[\s\S]*?generation: number/);
assert.match(taskPanel, /if \(identity !== actionIdentity\)[\s\S]*?closeTargetActions\(\)/,
  'session/provider replacement must close actions holding ids from the old list');
assert.match(taskPanel, /let actionOperationRevision = 0/);
assert.match(taskPanel, /function operationIsCurrent\([\s\S]*?targetIsCurrent\(operation\.target\)/);
for (const [name, next] of [
  ['handleParsePRD', 'handleAddTask'],
  ['handleAddTask', 'handleDeleteTask'],
  ['confirmDeleteTask', 'handleMoveTask'],
  ['handleMoveTask', 'handleExpandTask'],
  ['handleExpandTask', 'handleExpandAll'],
  ['handleAnalyzeComplexity', 'handleGetNextTask'],
  ['handleGetNextTask', 'handleSendToAgent'],
  ['handleSendToAgent', 'showContextMenu'],
  ['handleSaveEditTask', 'openAddSubtaskModal'],
  ['handleAddSubtask', 'handleToggleSubtaskStatus'],
  ['handleToggleSubtaskStatus', 'handleRemoveSubtask'],
  ['confirmRemoveSubtask', 'openDependencyModal'],
  ['handleAddDependency', 'handleRemoveDependency'],
  ['handleRemoveDependency', 'dependencyName'],
]) {
  const start = taskPanel.indexOf(`function ${name}`);
  const end = taskPanel.indexOf(`function ${next}`, start + 1);
  const body = start >= 0 && end > start ? taskPanel.slice(start, end) : '';
  assert.match(body, /await[\s\S]*operationIsCurrent\(/,
    `${name} must not mutate UI after a stale async completion`);
}
assert.match(tasks, /await App\.TaskMasterInit\(sessionId\);[\s\S]*?if \(isActiveTasksSession\(sessionId\)\) await checkTaskMasterStatus/,
  'late initialization must not reclaim the status store from the new session');

assert.match(diff, /type DiffTarget = \{[\s\S]*?root: string/);
assert.match(diff, /App\.RevertDiffFile\([\s\S]*?target\.root/);
assert.match(diff, /const key = `\$\{currentDiffKey\}:\$\{loadedRoot\}:\$\{wholeFileView/);

const statusColumn = taskPanel.match(/\.meta-column\.status-badge \{[\s\S]*?\n  \}/)?.[0] ?? '';
assert.match(statusColumn, /white-space: nowrap/);
const priorityColumn = taskPanel.match(/\.meta-column\.priority-badge \{[\s\S]*?\n  \}/)?.[0] ?? '';
assert.match(priorityColumn, /white-space: nowrap/);

console.log('frontendRuntimeRaces: ok');
