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

// A diff jump is consumed only after the target tree has initialized. Source
// order matters for independent Svelte reactive blocks, so assert that too.
const initAt = browser.indexOf('requestBrowseTarget(browseKey');
const jumpAt = browser.indexOf('loadedBrowseKey === browseKey && $pendingFileJump');
assert.ok(initAt >= 0 && jumpAt > initAt, 'target initialization must precede pending jump handling');

// Every diff identity includes the window and mode, including the per-file
// cache; a mode change explicitly drops local contents.
assert.match(diff, /requestedKey = `\$\{sessionId \|\| ''\}:\$\{windowIdx\}:\$\{mode\}`/);
assert.match(diff, /currentDiffKey = `\$\{\$selectedSessionId \|\| ''\}:\$\{\$selectedWindowIdx \?\? 0\}:\$\{diffMode\}`/);
assert.match(diff, /cacheKey = selectedPath[\s\S]*?`\$\{currentDiffKey\}:/);
assert.match(diff, /function handleModeChange[\s\S]*?fileCache = \{\}/);
assert.match(diff, /windowIdx !== tabIdx\(\)/, 'late file/list responses must be target-checked');

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
assert.match(notes, /<textarea[\s\S]*?disabled=\{loadingNotes\}/, 'the old note must not be editable under an incoming target');
assert.match(notes, /if \(loadingNotes\) return;[\s\S]*?recordHistory\(\)/, 'a synthetic input during loading must not queue a cross-tab save');

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
assert.match(
  tasks,
  /isActiveTasksSession\(sessionId\)[\s\S]*?await App\.GetTasks\(sessionId\)/,
  'an undo for a background session must not read the visible session store',
);

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

assert.match(diff, /type DiffTarget = \{[\s\S]*?root: string/);
assert.match(diff, /App\.RevertDiffFile\([\s\S]*?target\.root/);
assert.match(diff, /const key = `\$\{currentDiffKey\}:\$\{wholeFileView/);

const statusColumn = taskPanel.match(/\.meta-column\.status-badge \{[\s\S]*?\n  \}/)?.[0] ?? '';
assert.match(statusColumn, /white-space: nowrap/);
const priorityColumn = taskPanel.match(/\.meta-column\.priority-badge \{[\s\S]*?\n  \}/)?.[0] ?? '';
assert.match(priorityColumn, /white-space: nowrap/);

console.log('frontendRuntimeRaces: ok');
