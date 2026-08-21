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
const app = read('App.svelte');
const tabBar = read('lib/components/MainPanel/TabBar.svelte');
const newTabDialog = read('lib/components/Dialogs/NewTabDialog.svelte');
const newSessionDialog = read('lib/components/Dialogs/NewSessionDialog.svelte');
const quickTerminalDialog = read('lib/components/Dialogs/QuickTerminalDialog.svelte');
const confirmDialog = read('lib/components/Dialogs/ConfirmDialog.svelte');
const newGroupDialog = read('lib/components/Dialogs/NewGroupDialog.svelte');
const forkDialog = read('lib/components/Dialogs/ForkDialog.svelte');
const logDialog = read('lib/components/Dialogs/LogDialog.svelte');
const sessionFileDialog = read('lib/components/Dialogs/SessionFileDialog.svelte');
const updateDialog = read('lib/components/Dialogs/UpdateDialog.svelte');
const schemeImport = read('lib/components/Dialogs/SchemeImportDialog.svelte');
const mainPanel = read('lib/components/MainPanel/MainPanel.svelte');
const sideBySideDiff = read('lib/components/MainPanel/SideBySideDiff.svelte');
const sessionItem = read('lib/components/Sidebar/SessionItem.svelte');
const projectSelector = read('lib/components/Sidebar/ProjectSelector.svelte');
const commandPalette = read('lib/components/Dialogs/CommandPalette.svelte');
const sessionStore = read('lib/stores/sessions.ts');
const projectStore = read('lib/stores/projects.ts');
const settingsStore = read('lib/stores/settings.ts');
const sidebarPolling = read('lib/stores/sidebarPolling.ts');
const dialogActions = read('lib/utils/dialogActions.ts');
const importDialog = read('lib/components/Dialogs/ImportDialog.svelte');
const globalSearch = read('lib/components/Dialogs/GlobalSearchDialog.svelte');
const commandPicker = read('lib/components/Dialogs/CommandPickerDialog.svelte');
const resumePicker = read('lib/components/Dialogs/ResumeSessionPickerDialog.svelte');
const gitHistory = read('lib/components/Dialogs/GitHistoryDialog.svelte');
const bgAgents = read('lib/components/Dialogs/BgAgentsDialog.svelte');
const recovery = read('lib/components/Dialogs/RecoveryCenterDialog.svelte');
const allTasks = read('lib/components/Dashboard/AllTasks.svelte');
const settingsDialog = read('lib/components/Dialogs/SettingsDialog.svelte');
const sessionTemplates = read('lib/components/Dialogs/SessionTemplateDialog.svelte');
const saveAsTemplate = read('lib/components/Dialogs/SaveAsTemplateDialog.svelte');
const commandManager = read('lib/components/Dialogs/CommandManagerDialog.svelte');
const i18n = read('lib/i18n/index.ts');

assert.match(
  recovery,
  /await flushSettingsSaves\(\);[\s\S]*?if \(!operationIsCurrent\(target\)\) return;[\s\S]*?await App\.RestoreBackup/,
  'a full backup restore must revalidate its project after settings flush and before the destructive backend call',
);
assert.match(
  recovery,
  /const result = await App\.RestoreTrashItem\(item\.id\);[\s\S]*?if \(!operationIsCurrent\(target\)\) return;[\s\S]*?await loadSessions\(\)/,
  'a restore completion from an old project must not refresh or select into the replacement project',
);
assert.match(recovery, /loadedProjectId !== \$activeProjectId/,
  'an open Recovery view must replace rows when the active project changes');
assert.match(recovery, /projectId !== \$activeProjectId/,
  'a late Recovery list response must not populate the replacement project');

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
assert.match(notes, /registerUnsavedGuard\(\{[\s\S]*?isDirty: hasUnsavedDrafts/,
  'failed drafts hidden on another tab must participate in the global destructive-action guard');
assert.match(notes, /\[\.\.\.draftsByTarget\.values\(\)\]\.some/);

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
assert.match(removeTask, /captureActiveTasksTarget\(sessionId\)/);
assert.match(removeTask, /activeTasksTargetIsCurrent\(target\)/);
assert.match(tasks, /async function reloadTasksIfActive/);
assert.match(tasks, /const localMutationQueues = new Map/);
const localEdit = tasks.match(/async function editTaskLocally[\s\S]*?\n\}/)?.[0] ?? '';
assert.match(localEdit, /const queueKey = target \? `\$\{target\.generation\}\\x1f\$\{sessionId\}` : sessionId/);
assert.match(localEdit, /activeTasksTargetIsCurrent\(target\)/,
  'a queued local read-modify-write must fail closed after its project/session context is invalidated');
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
assert.match(diff, /target\.root === loadedRoot && loadedDiffKey === currentDiffKey/,
  'revert identity must include the canonical tree that produced the displayed diff');
const confirmRevert = diff.match(/async function confirmRevert\(\)[\s\S]*?\n  \}/)?.[0] ?? '';
assert.match(confirmRevert, /await action\.run\(\);[\s\S]*?!isCurrentTarget\(action\.target\)/,
  'a completed revert must not refresh or reposition a replacement diff target');

// Confirmation dialogs must hold the exact session/tab they described. The
// selection is allowed to change while the modal or a prerequisite await is
// open, so reading the live store in the confirm handler is destructive.
assert.match(app, /let pendingDeleteTarget:/);
assert.match(app, /afterUnsavedChanges\(\(\) => \{ void deleteSession\(target\.id\); \}\)/,
  'session deletion must wait for every active unsaved editor');
assert.match(app, /let pendingStopTarget: SessionDialogTarget/);
assert.match(app, /await stopSession\(target\.sessionId\)/);
assert.match(app, /await stopTab\(target\.sessionId, target\.windowIdx\)/);
assert.match(app, /let pendingStartTarget: SessionDialogTarget/);
assert.match(app, /await startSession\(target\.sessionId\)/);
assert.match(app, /await restartTab\(target\.sessionId, target\.windowIdx\)/);
assert.match(app, /resolveGitHistoryPath\(session: Session, windowIdx: number\)/);
assert.match(app, /const path = await resolveGitHistoryPath\(session, winIdx\);[\s\S]*?pendingResumeSession\?\.id !== session\.id/,
  'a delayed history-path lookup must not reopen the resume picker for the previous tab');
assert.match(app, /pendingResumeSession &&[\s\S]*?showResumeSessionPicker = false;[\s\S]*?handleResumeCancel\(\)/,
  'resume dialogs must close when their captured session/tab is no longer selected');
assert.match(app, /let resumeOperationGeneration = 0/);
assert.match(app, /const target = pendingResumeSession;[\s\S]*?const generation = resumeOperationGeneration;[\s\S]*?await[\s\S]*?generation === resumeOperationGeneration[\s\S]*?handleResumeCancel\(\)/,
  'a late resume completion must not clear a replacement session/tab picker');
assert.match(app, /let quickJumpNamingGeneration = 0/);
assert.match(app, /const targetSession = quickJumpTargetSession;[\s\S]*?await AddQuickJump\(targetSession, targetWindow,[\s\S]*?generation !== quickJumpNamingGeneration \|\| projectId !== \$activeProjectId \|\| anyDialogOpen/,
  'a late quick-jump add must not reopen its list over a replacement dialog or project');
assert.match(tabBar, /let deleteSessionTarget:/);
assert.match(tabBar, /afterUnsavedChanges\(\(\) => \{ void deleteCapturedSession\(target\); \}\)/);
assert.match(tabBar, /let deleteTabTarget:/);
assert.match(tabBar, /await deleteTab\(target\.sessionId, target\.windowIdx\)/);
assert.match(tabBar, /afterUnsavedChanges\(\(\) => \{ void deleteCapturedTab\(target\); \}\)/);
assert.match(tabBar, /let extraArgsTarget:/);
assert.match(tabBar, /App\.SetExtraArgs\(target\.sessionId, target\.windowIdx/);
assert.match(tabBar, /let renameTarget:/);
assert.match(tabBar, /await renameTab\(target\.sessionId, target\.windowIdx/);
assert.match(tabBar, /let tabColorSessionId = ''/);
assert.match(tabBar, /sessionId=\{tabColorSessionId\}/,
  'the tab color dialog must not receive the live selected session beside an old tab snapshot');
assert.match(tabBar, /sessionId=\{newTabSessionId\}/);
assert.match(tabBar, /sessionId=\{quickTerminalSessionId\}/);
assert.match(newTabDialog, /App\.CreateTab\(targetSessionId/);
assert.match(newTabDialog, /get\(selectedSessionId\) === targetSessionId/);
assert.match(newSessionDialog, /const generation = \+\+resumeLookupGeneration/);
assert.match(newSessionDialog, /clearTimeout\(pathDebounceTimer\)/,
  'closing or replacing the new-session target must cancel its resume debounce');
assert.match(newSessionDialog, /generation !== resumeLookupGeneration \|\| path !== searchPath \|\| selectedAgent !== agent/,
  'resume candidates must belong to the open/path/agent that requested them');
assert.match(quickTerminalDialog, /App\.CreateTab\(targetSessionId/);
assert.match(quickTerminalDialog, /get\(selectedSessionId\) === targetSessionId/);

assert.match(sessionItem, /<ConfirmDialog[\s\S]*?on:confirm=\{confirmDelete\}/,
  'the sidebar delete affordance must not delete a session on its first click');
assert.match(sessionItem, /afterUnsavedChanges\(\(\) => \{ void deleteSession\(target\.id\); \}\)/);

// Project switches invalidate both list reads and mutation continuations. A
// repeated A -> B -> A sequence is why a request counter is required in
// addition to comparing session ids.
assert.match(sessionStore, /export function invalidateSessionProject\(\)/);
assert.match(sessionStore, /const generation = \+\+sessionsLoadGeneration/);
assert.match(sessionStore, /!projectTargetIsCurrent\(target\) \|\| generation !== sessionsLoadGeneration/);
assert.match(projectStore, /sessionStore\.invalidateSessionProject\(\);[\s\S]*?App\.SelectProject\(id\)/,
  'old-project requests must be invalidated before the backend identity changes');
assert.match(sessionStore, /sessionTabMemory\.clear\(\);[\s\S]*?selectedSessionId\.set\(null\)/,
  'project invalidation must clear selection and same-id tab memory without persisting through the new backend');
assert.match(projectStore, /afterUnsavedChanges\([\s\S]*?selectProjectNow\(id\)[\s\S]*?resolve\(false\)/,
  'a project switch must remain outside the backend queue until every dirty editor approves');
assert.match(projectSelector, /if \(await selectProject\(id\)\) \{[\s\S]*?showDashboard\(\)/,
  'cancelling the project switch must not navigate away from the editor');
assert.match(projectSelector, /if \(!\(await selectProject\(project\.id\)\)\) \{[\s\S]*?newProjectName = '';[\s\S]*?isCreating = false/,
  'a created project must not be duplicated when its initial switch is cancelled');
assert.match(commandPalette, /if \(await selectProject\(project\.id\)\) showDashboard\(\)/,
  'palette project cancellation must not navigate away from the editor');
assert.match(projectStore, /catch \(e\)[\s\S]*?App\.GetActiveProjectID\(\)[\s\S]*?sessionStore\.loadSessions\(\)/,
  'a failed project selection must reload the still-active backend project after invalidation');
assert.match(projectStore, /dismissUndo\(\);[\s\S]*?App\.SelectProject\(id\)/,
  'a project-scoped task undo must not survive into a project with colliding session/task ids');
for (const mutation of ['renameSession', 'toggleFavorite', 'setSessionColor', 'assignToGroup', 'deleteSession']) {
  const start = sessionStore.indexOf(`export async function ${mutation}`);
  const next = sessionStore.indexOf('\nexport ', start + 1);
  const body = start >= 0 ? sessionStore.slice(start, next > start ? next : undefined) : '';
  assert.match(body, /const target = projectTarget\(\)/, `${mutation} must capture project identity`);
  assert.match(body, /projectTargetIsCurrent\(target\)/, `${mutation} must reject stale completion`);
}

assert.match(settingsStore, /const revision = \+\+settingsRevision/);
assert.match(settingsStore, /await loadSettings\(revision\)/);
assert.match(settingsStore, /expectedRevision !== undefined && expectedRevision !== settingsRevision/,
  'a recovery read for a failed save must not overwrite a newer optimistic edit');

// Long-lived dialogs must invalidate async results when their target changes
// or they close; several are mounted once by App and only toggle `show`.
assert.match(importDialog, /const generation = \+\+requestGeneration/);
assert.match(importDialog, /selectedProjectId !== projectId/,
  'a late import-source response must not replace the newly selected project');
assert.match(globalSearch, /function clearQuery\(\)[\s\S]*?handleQueryChange\(\)/,
  'clearing search must use the same request invalidation as typing');
assert.match(globalSearch, /App\.GetHistoryPreview\(entry\.id\)/,
  'history previews must use the backend-issued opaque id, not displayed path metadata');
assert.match(sessionFileDialog, /App\.ImportSessionFile\(targetToken, targetSelection\)/,
  'portable imports must consume the backend snapshot token instead of reopening a displayed path');
assert.match(commandPicker, /targetSessionId = sessionId;[\s\S]*?targetWindowIdx = windowIdx/);
assert.match(commandPicker, /if \(running \|\| !targetSessionId\) return/,
  'a command must have one captured target and one in-flight execution');
assert.match(commandPicker, /sessionId !== targetSessionId \|\| windowIdx !== targetWindowIdx\)\) close\(\)/,
  'a command picker must close when its captured tab is no longer selected');
assert.match(resumePicker, /generation !== loadGeneration \|\| key !== lastLoadKey/);
assert.match(gitHistory, /repositoryGeneration/);
assert.match(gitHistory, /projectId.*?sessionId.*?windowIdx.*?path/s,
  'git history identity must include project, session, tab and expected root');
assert.match(gitHistory, /App\.GetGitHistory\([\s\S]*?target\.sessionId[\s\S]*?target\.windowIdx, target\.root/,
  'git history reads must carry the captured tab and expected root to the backend');
assert.match(mainPanel, /const target = `\$\{\$activeProjectId\}:\$\{\$selectedSessionId \|\| ''\}:\$\{\$selectedWindowIdx \?\? 0\}`/,
  'live tab cwd identity must include the project when session ids are reused');
assert.match(gitHistory, /generation !== historyGeneration \|\| branch !== targetBranch/);
assert.match(gitHistory, /generation !== commitGeneration \|\| selectedHash !== hash/);
assert.match(gitHistory, /generation !== diffGeneration \|\| selectedHash !== hash \|\| selectedPath !== file/);
assert.match(bgAgents, /if \(!attachFor \|\| attaching\) return/);
assert.match(bgAgents, /generation !== logsGeneration \|\| logsFor !== agent\.id/);
assert.match(recovery, /guardUnsaved: true/);
assert.match(recovery, /afterUnsavedChanges\(\(\) => \{ void execute\(\); \}\)/,
  'backup restore must wait for dirty editors before replacing project state');
assert.match(recovery, /window\.dispatchEvent\(new CustomEvent\('tasks:refresh'\)\)/,
  'restoring task files must invalidate already-mounted task views');
assert.match(taskPanel, /const refresh = \(\) => \{ if \(active\) void loadTasksIfNeeded\(true\); \}/);
assert.match(taskPanel, /addEventListener\('tasks:refresh', refresh\)/);
assert.match(recovery, /if \(!operationUIIsCurrent\(target\)\)/,
  'a portalled confirmation must not run against a replacement active project');
assert.match(updateDialog, /function close\(\) \{[\s\S]*?if \(isUpdating\) return;/,
  'an in-progress installation must not be dismissible while the updater owns application files');
assert.match(allTasks, /task\.projectId !== \$activeProjectId[\s\S]*?await selectProject\(task\.projectId\)/,
  'an all-project task jump must switch the backend project before selecting its session id');
assert.match(allTasks, /const key = `\$\{task\.projectId\}:\$\{task\.sessionId \|\| task\.projectPath\}`/,
  'task groups must not merge project-scoped session ids');
assert.match(settingsDialog, /else if \(!show && previousShow\) \{[\s\S]*?showApiKey = false/,
  'a revealed API key must be covered when the persistently mounted dialog closes');
assert.match(settingsDialog, /dictationLoadGeneration\+\+;[\s\S]*?cancelAudioTest\(\)/,
  'closing settings must invalidate delayed device reads and audio countdowns');
assert.match(settingsDialog, /if \(!show \|\| generation !== audioTestGeneration\) return/,
  'a closed audio-test countdown must not start microphone playback later');
assert.match(settingsStore, /export function invalidateSettingsContext/);
assert.match(settingsStore, /context !== settingsContextGeneration \|\| generation !== settingsLoadGeneration/,
  'settings reads must belong to the project context that started them');
assert.match(projectStore, /settingsStore\.invalidateSettingsContext\(\)/,
  'project switching must invalidate settings reads and queued snapshots');
assert.match(projectStore, /generation !== lockLoadGeneration \|\| projectId !== get\(activeProjectId\)/,
  'a lock response must belong to the project whose mutation controls it gates');
assert.match(app, /await flushSettingsSaves\(\);[\s\S]*?Quit\(\)/,
  'shutdown must drain settings snapshots before tearing down Wails');
assert.match(app, /appMounted = false;[\s\S]*?stopSidebarPolling\(\)/,
  'unmount must invalidate async startup before tearing down its listeners');
assert.match(sidebarPolling, /export function invalidateSidebarProject[\s\S]*?activities\.set\(\{\}\)[\s\S]*?tabStatuses\.set\(\{\}\)/,
  'project switches must clear both sidebar payloads and their equality cache');
assert.match(sessionStore, /invalidateSidebarProject\(\)/);
assert.match(dialogActions, /cancelAnimationFrame\(frame\)[\s\S]*?removeEventListener\('keydown', trapTab\)/,
  'dialog focus actions must release delayed focus and keyboard traps');
assert.match(gitHistory, /activeDocumentDragCleanup\?\.\(\);[\s\S]*?invalidateRequests\(\)/,
  'closing history must remove document drag listeners before hiding');
assert.match(settingsDialog, /const generation = \+\+languageChangeGeneration;[\s\S]*?generation !== languageChangeGeneration/,
  'a slow earlier translation load must not win over the last language choice');
assert.match(i18n, /const generation = \+\+translationLoadGeneration;[\s\S]*?generation !== translationLoadGeneration/,
  'late dynamic locale chunks must not replace the most recently selected language');
assert.match(settingsDialog, /const save = dictationSaveQueue[\s\S]*?SetDictationSettings\(snapshot\)/,
  'whole-object dictation settings writes must be serialized');
assert.match(sessionTemplates, /fName\.trim\(\)\.length > 0 && !saving/);
assert.match(sessionTemplates, /generation !== operationGeneration \|\| mode !== 'edit' \|\| editingId !== targetEditingId/,
  'a late template save must not close a replacement editor');
assert.match(sessionTemplates, /picked && show && generation === operationGeneration && mode === 'edit'/,
  'a native directory picker must belong to the template editor that opened it');
assert.match(sessionTemplates, /await App\.DeleteSessionTemplate\(target\.id\);[\s\S]*?generation !== operationGeneration/,
  'a completed template delete must not mutate a replacement manager cycle');
assert.match(saveAsTemplate, /session\?\.id !== targetSessionId/,
  'save-as-template completion must belong to the captured session');
assert.match(commandManager, /let savingCommand = false/);
assert.match(commandManager, /let savingGroup = false/);
assert.match(commandManager, /generation !== operationGeneration \|\| !editing \|\| editingId !== targetId/,
  'a late command save must not close a replacement editor');
assert.match(schemeImport, /let requestGeneration = 0/);
for (const call of ['DiscoverLocalSchemes', 'ImportSchemeFiles', 'ListOnlineSchemes', 'FetchOnlineSchemes']) {
  assert.match(schemeImport, new RegExp(`await App\\.${call}[\\s\\S]*?generation !== requestGeneration \\|\\| tab !== targetTab`),
    `${call} must not populate a different scheme-source tab or open cycle`);
}
assert.match(schemeImport, /function switchTab[\s\S]*?requestGeneration\+\+/,
  'switching scheme sources must invalidate the previous source request');

// Enter bubbles out of native buttons. The safe button in a destructive
// confirmation must retain its native action, and single-field/form dialogs
// must not submit once at the field and again at the overlay/form.
assert.match(confirmDialog, /e\.key === 'Enter' && !dialogEnterBelongsToControl\(e\)/);
assert.doesNotMatch(quickTerminalDialog, /<input[\s\S]*?on:keydown=/,
  'QuickTerminal input and overlay must not both submit the same Enter');
assert.match(quickTerminalDialog, /if \(isSubmitting\) return/);
assert.match(newTabDialog, /<form on:submit\|preventDefault=\{handleSubmit\}>/);
assert.doesNotMatch(newTabDialog, /e\.key === 'Enter'/,
  'NewTab native form submit must be the sole Enter path');
assert.match(newTabDialog, /dir && show && generation === operationGeneration && sessionId === targetSessionId/,
  'a native tab work-directory picker must not overwrite a replacement session form');
assert.match(forkDialog, /<form on:submit\|preventDefault=\{handleSubmit\}>/);
assert.doesNotMatch(forkDialog, /e\.key === 'Enter'/,
  'Fork native form submit must be the sole Enter path');
for (const [name, source] of [
  ['NewGroup', newGroupDialog], ['QuickTerminal', quickTerminalDialog],
  ['Fork', forkDialog], ['Log', logDialog], ['SessionFile', sessionFileDialog],
  ['Update', updateDialog], ['NewSession', newSessionDialog],
]) {
  assert.match(source, /operationGeneration|loadGeneration|requestGeneration/,
    `${name} must identify close/reopen async operations`);
}
assert.match(newSessionDialog, /const resumeId = selectedResumeId/);
assert.match(newSessionDialog, /selectedPath && show && generation === operationGeneration && path === initialPath/,
  'a native session directory picker must not overwrite a replacement form');

assert.match(mainPanel, /onDestroy\(\(\) => \{[\s\S]*?activeSplitterCleanup\?\.\(\)/,
  'unmounting MainPanel mid-drag must release window listeners');
assert.match(sideBySideDiff, /onDestroy\(\(\) => activeSplitCleanup\?\.\(\)\)/,
  'unmounting a side-by-side diff mid-drag must release document listeners');

const statusColumn = taskPanel.match(/\.meta-column\.status-badge \{[\s\S]*?\n  \}/)?.[0] ?? '';
assert.match(statusColumn, /white-space: nowrap/);
const priorityColumn = taskPanel.match(/\.meta-column\.priority-badge \{[\s\S]*?\n  \}/)?.[0] ?? '';
assert.match(priorityColumn, /white-space: nowrap/);

console.log('frontendRuntimeRaces: ok');
