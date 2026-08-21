import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { readFileSync } from 'node:fs';

const root = new URL('../', import.meta.url);

async function bundlePool() {
  const result = await build({
    entryPoints: [new URL('src/lib/utils/terminalPool.ts', root).pathname],
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'node',
    plugins: [{
      name: 'terminal-pool-mocks',
      setup(api) {
        api.onResolve({ filter: /\/terminal$/ }, () => ({ path: 'terminal', namespace: 'mock' }));
        api.onLoad({ filter: /^terminal$/, namespace: 'mock' }, () => ({
          contents: `
            export const createTerminal = () => ({});
            export const attachToSession = async () => {};
            export const detachFromSession = async () => {};
            export const fitTerminal = () => {};
            export const resendTerminalSize = () => {};
            export const sendVisibility = () => {};
            export const clearAwaitingRedraw = () => {};
            export const themeFor = () => ({});
            export const fontSizeFor = () => 14;
            export const terminalFontStack = () => 'monospace';
          `,
          loader: 'js',
        }));
        api.onResolve({ filter: /wailsjs\/go\/main\/App$/ }, () => ({ path: 'app', namespace: 'mock' }));
        api.onLoad({ filter: /^app$/, namespace: 'mock' }, () => ({
          contents: `export const LogFrontend = () => Promise.resolve(); export const RedrawWindow = () => Promise.resolve();`,
          loader: 'js',
        }));
      },
    }],
  });
  return import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}#${Date.now()}`);
}

let listener = null;
let added = 0;
let removed = 0;
globalThis.window = {
  devicePixelRatio: 1,
  matchMedia: () => ({
    addEventListener(_type, fn) { listener = fn; added++; },
    removeEventListener(_type, fn) { if (listener === fn) listener = null; removed++; },
  }),
};
globalThis.requestAnimationFrame = () => 1;
globalThis.cancelAnimationFrame = () => {};

const { TerminalPool } = await bundlePool();
const pool = new TerminalPool({});
assert.equal(added, 1);

// A delayed teardown for a hidden old session must not invalidate the visible
// session's in-flight show generation.
pool.showGeneration = 7;
pool.activeKey = 'new-session:2';
pool.entries.set('old-session:1', {
  key: 'old-session:1',
  containerEl: { remove() {} },
  terminalInstance: { cleanup() {} },
  themeCtx: {},
});
await pool.destroy('old-session');
assert.equal(pool.showGeneration, 7);
assert.equal(pool.activeKey, 'new-session:2');

await pool.dispose();
assert.equal(listener, null, 'dispose must release the armed matchMedia listener');
assert.equal(removed, 1);

delete globalThis.window;
delete globalThis.requestAnimationFrame;
delete globalThis.cancelAnimationFrame;

// Component-level contracts for stale async continuations and duplicate input.
const terminal = readFileSync(new URL('src/lib/components/MainPanel/Terminal.svelte', root), 'utf8');
const tabBar = readFileSync(new URL('src/lib/components/MainPanel/TabBar.svelte', root), 'utf8');
const browser = readFileSync(new URL('src/lib/components/MainPanel/FileBrowser.svelte', root), 'utf8');
const quickOpen = readFileSync(new URL('src/lib/components/MainPanel/FileQuickOpen.svelte', root), 'utf8');
const quickJump = readFileSync(new URL('src/lib/components/Dialogs/QuickJumpDialog.svelte', root), 'utf8');
const palette = readFileSync(new URL('src/lib/components/Dialogs/CommandPalette.svelte', root), 'utf8');
const mainPanel = readFileSync(new URL('src/lib/components/MainPanel/MainPanel.svelte', root), 'utf8');
const taskPanel = readFileSync(new URL('src/lib/components/MainPanel/TaskPanel.svelte', root), 'utf8');

assert.match(terminal, /stillViewingStoppedSession[\s\S]*?if \(stillViewingStoppedSession\) pool\.hideAll\(\)/);
assert.match(terminal, /const targetWindowIdx = currentTargetWindowIdx\(\)[\s\S]*?currentTargetWindowIdx\(\) !== targetWindowIdx/);
assert.match(terminal, /resetFontSizeForCurrentTab\(e\.detail, focusOwner\)/);
assert.match(terminal, /const projectId = get\(activeProjectId\)[\s\S]*?if \(get\(activeProjectId\) !== projectId\) return;[\s\S]*?SetTabFontSize\(sid, widx, size, projectId\)/,
  'a debounced font-size save must remain pinned to the project where the gesture occurred');
assert.match(terminal, /SetTabFontSize\(sid, widx, 0, projectId\)[\s\S]*?get\(activeProjectId\) !== projectId/,
  'font-size reset and its refresh must remain pinned to one project');
assert.match(tabBar, /if \(!bufferText\.trim\(\) \|\| bufferBusy\) return/);
assert.match(tabBar, /const widx = get\(selectedWindowIdx\)[\s\S]*?SendPromptToWindow\(sid, widx, submitted\)/);
assert.match(tabBar, /const queued = previous[\s\S]*?SetBufferText\(submitted\)/,
  'dictation-buffer writes must be serialized');
assert.match(tabBar, /await bufferSyncQueue\.catch[\s\S]*?SendPromptToWindow/,
  'send must wait for old editor syncs before clearing the backend buffer');
assert.match(tabBar, /SendPromptToWindow\(sid, widx, submitted\)[\s\S]*?bufferText = ''[\s\S]*?ClearBuffer\(\)/,
  'a committed prompt must stop being sendable before fallible buffer cleanup');
assert.match(tabBar, /if \(!componentMounted\) return;[\s\S]*?EventsOn\('dictation:state'/,
  'a late initial settings read must not register listeners after teardown');
assert.match(tabBar, /await App\.SetExtraArgs\([\s\S]*?target === extraArgsTarget && target\.generation === extraArgsGeneration[\s\S]*?showExtraArgsEditor = false/,
  'a late extra-args save must not close a replacement editor');
assert.match(tabBar, /showExtraArgsEditor\}[\s\S]*?class="dialog-overlay" use:autoFocusDialog/,
  'the tab extra-args modal must keep keyboard focus inside the editor');
assert.match(tabBar, /const generation = \+\+renameGeneration[\s\S]*?await renameTab\([\s\S]*?target === renameTarget && target\.generation === renameGeneration[\s\S]*?renamingTabIndex = null/,
  'a late tab rename must not close a replacement rename draft');
assert.match(tabBar, /onDestroy\(\(\) => \{[\s\S]*?removeEventListener\('mousemove', onDragMove\)[\s\S]*?removeEventListener\('mousemove', onResizeMove\)/);
assert.match(quickOpen, /await App\.InvalidateSessionFileIndex\(targetSessionId\)[\s\S]*?await load\(\)/);
assert.match(browser, /await App\.InvalidateSessionFileIndex\(sessionId\)[\s\S]*?await loadDir\('', true\)[\s\S]*?for \(const path of open\)/);
assert.match(quickJump, /let loadGeneration = 0/);
assert.match(quickJump, /if \(mutationPending\) return/);
assert.match(palette, /generation === templateLoadGeneration && projectId === \$activeProjectId/);
assert.match(mainPanel, /`\$\{\$activeProjectId\}:\$\{currentSession\.id\}:\$\{\$selectedWindowIdx\}`/,
  'the optimistic notes preview must not cross project identity');
assert.match(browser, /browseKey = `\$\{\$activeProjectId\}:\$\{\$selectedSessionId/,
  'file and scroll memory must include the project');
assert.match(taskPanel, /const targetKey = `\$\{projectId\}:\$\{sessionId \?\? ''\}`/);
assert.match(taskPanel, /await checkTaskMasterStatus\(''\)[\s\S]*?await loadTasks\(''\)/,
  'clearing the task target must invalidate both task and provider-status requests');

console.log('terminalLifecycle: ok');
