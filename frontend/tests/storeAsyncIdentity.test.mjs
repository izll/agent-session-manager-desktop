import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { build } from 'esbuild';

const root = new URL('../', import.meta.url);
const originalConsoleError = console.error;
console.error = () => {};

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

async function eventually(predicate) {
  for (let i = 0; i < 100; i++) {
    if (predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  throw new Error('condition did not become true');
}

async function bundleStore(entry, globalName) {
  const bindings = readFileSync(new URL('wailsjs/go/main/App.d.ts', root), 'utf8');
  const appMethods = [...new Set([...bindings.matchAll(/export function ([A-Za-z0-9_]+)/g)].map((match) => match[1]))];
  const result = await build({
    entryPoints: [new URL(entry, root).pathname],
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'node',
    plugins: [{
      name: 'store-race-mocks',
      setup(buildApi) {
        buildApi.onResolve({ filter: /wailsjs\/go\/main\/App$/ }, () => ({ path: 'app', namespace: 'race' }));
        buildApi.onLoad({ filter: /^app$/, namespace: 'race' }, () => ({
          contents: appMethods.map((name) =>
            `export const ${name} = (...args) => globalThis.${globalName}.${name}(...args);`
          ).join('\n'),
          loader: 'js',
        }));
        buildApi.onResolve({ filter: /utils\/terminal$/ }, () => ({ path: 'terminal', namespace: 'race' }));
        buildApi.onLoad({ filter: /^terminal$/, namespace: 'race' }, () => ({
          contents: `export const defaultTerminalRenderer = () => 'dom';`,
          loader: 'js',
        }));
      },
    }],
  });
  const encoded = Buffer.from(result.outputFiles[0].text).toString('base64');
  return import(`data:text/javascript;base64,${encoded}#${Date.now()}-${Math.random()}`);
}

// A failed older save starts a recovery read while the queued newer save is
// succeeding. The recovery response is deliberately the old disk snapshot and
// arrives last; it must not roll the store back.
{
  const firstSave = deferred();
  const secondSave = deferred();
  const staleRecoveryRead = deferred();
  const saves = [];
  globalThis.__settingsRaceApp = {
    SaveSettings(value) {
      saves.push(structuredClone(value));
      return saves.length === 1 ? firstSave.promise : secondSave.promise;
    },
    GetSettings() { return staleRecoveryRead.promise; },
    LogFrontend() { return Promise.resolve(); },
  };
  const store = await bundleStore('src/lib/stores/settings.ts', '__settingsRaceApp');
  let visible;
  store.settings.subscribe((value) => { visible = value; });

  const older = store.saveSettings({ language: 'hu' });
  const newer = store.saveSettings({ uiTheme: 'ocean' });
  await eventually(() => saves.length === 1);
  firstSave.reject(new Error('disk full'));
  await eventually(() => saves.length === 2);
  secondSave.resolve();
  await newer;
  staleRecoveryRead.resolve({ ...saves[0], uiTheme: 'stale' });
  await older;

  assert.equal(visible.language, 'hu');
  assert.equal(visible.uiTheme, 'ocean');
  delete globalThis.__settingsRaceApp;
}

// A project switch invalidates an old list request and an old mutation. Even
// when the new project reuses the same session id, the late completions must
// not overwrite its cards.
{
  const oldSessions = deferred();
  const oldGroups = deferred();
  const rename = deferred();
  let listCall = 0;
  globalThis.__sessionsRaceApp = new Proxy({
    GetSessions() {
      listCall++;
      return listCall === 1 ? oldSessions.promise : Promise.resolve([{ id: 'same', name: 'new project' }]);
    },
    GetGroups() { return listCall === 1 ? oldGroups.promise : Promise.resolve([]); },
    RenameSession() { return rename.promise; },
  }, {
    get(target, key) {
      if (key in target) return target[key];
      return async () => undefined;
    },
  });
  const store = await bundleStore('src/lib/stores/sessions.ts', '__sessionsRaceApp');
  let visible = [];
  let selectedId = null;
  let selectedWindow = 0;
  store.sessions.subscribe((value) => { visible = value; });
  store.selectedSessionId.subscribe((value) => { selectedId = value; });
  store.selectedWindowIdx.subscribe((value) => { selectedWindow = value; });

  const staleLoad = store.loadSessions();
  store.invalidateSessionProject();
  await store.loadSessions();
  assert.equal(visible[0].name, 'new project');
  oldSessions.resolve([{ id: 'same', name: 'old project' }]);
  oldGroups.resolve([]);
  await staleLoad;
  assert.equal(visible[0].name, 'new project');

  const staleRename = store.renameSession('same', 'old rename');
  store.invalidateSessionProject();
  store.sessions.set([{ id: 'same', name: 'new project remains' }]);
  rename.resolve();
  await staleRename;
  assert.equal(visible[0].name, 'new project remains');

  // Tab memory and selection are project-scoped even when two projects reuse
  // the same session id. Invalidation must not persist the old tab against the
  // backend after its active project changes, or reopen that tab in the new
  // project.
  store.sessions.set([{ id: 'same', name: 'old project', mainWindowIndex: 0,
    lastWindowIndex: 3, followedWindows: [{ index: 3 }, { index: 4 }] }]);
  store.selectSession('same');
  store.selectWindow(4);
  store.invalidateSessionProject();
  assert.equal(selectedId, null);
  assert.equal(selectedWindow, 0);
  store.sessions.set([{ id: 'same', name: 'new project', mainWindowIndex: 0,
    lastWindowIndex: 1, followedWindows: [{ index: 1 }] }]);
  store.selectSession('same');
  assert.equal(selectedWindow, 1);
  delete globalThis.__sessionsRaceApp;
}

// Toggle endpoints return no desired value. If a concurrent refresh observes
// the backend's new value before the toggle promise settles, blindly inverting
// the current store at completion reverses that fresh snapshot and leaves the
// UI disagreeing with disk. Each toggle must finish from a backend reload.
{
  let favorite = false;
  let autoYes = false;
  let collapsed = false;
  let pendingToggle = deferred();
  globalThis.__sessionToggleRaceApp = new Proxy({
    GetSessions: async () => [{ id: 'session-1', name: 'one', favorite, autoYes }],
    GetGroups: async () => [{ id: 'group-1', name: 'one', collapsed }],
    ToggleFavorite() { favorite = !favorite; return pendingToggle.promise; },
    ToggleAutoYes() { autoYes = !autoYes; return pendingToggle.promise; },
    ToggleGroupCollapse() { collapsed = !collapsed; return pendingToggle.promise; },
  }, {
    get(target, key) {
      if (key in target) return target[key];
      return async () => undefined;
    },
  });
  const store = await bundleStore('src/lib/stores/sessions.ts', '__sessionToggleRaceApp');
  let visibleSessions = [];
  let visibleGroups = [];
  store.sessions.subscribe((value) => { visibleSessions = value; });
  store.groups.subscribe((value) => { visibleGroups = value; });
  await store.loadSessions();

  let operation = store.toggleFavorite('session-1');
  await store.loadSessions();
  assert.equal(visibleSessions[0].favorite, true);
  pendingToggle.resolve();
  await operation;
  assert.equal(visibleSessions[0].favorite, true);

  pendingToggle = deferred();
  operation = store.toggleAutoYes('session-1');
  await store.loadSessions();
  assert.equal(visibleSessions[0].autoYes, true);
  pendingToggle.resolve();
  await operation;
  assert.equal(visibleSessions[0].autoYes, true);

  pendingToggle = deferred();
  operation = store.toggleGroupCollapse('group-1');
  await store.loadSessions();
  assert.equal(visibleGroups[0].collapsed, true);
  pendingToggle.resolve();
  await operation;
  assert.equal(visibleGroups[0].collapsed, true);
  delete globalThis.__sessionToggleRaceApp;
}

// A failed settings save may still be reading its old project's recovery
// snapshot while the user switches projects. That late read must not replace
// the settings already loaded for the new project.
{
  const failedSave = deferred();
  const oldRecovery = deferred();
  const newProjectRead = deferred();
  let reads = 0;
  globalThis.__settingsProjectRaceApp = {
    SaveSettings() { return failedSave.promise; },
    GetSettings() { return ++reads === 1 ? oldRecovery.promise : newProjectRead.promise; },
    LogFrontend() { return Promise.resolve(); },
  };
  const store = await bundleStore('src/lib/stores/settings.ts', '__settingsProjectRaceApp');
  let visible;
  store.settings.subscribe((value) => { visible = value; });

  const oldSave = store.saveSettings({ language: 'hu' });
  failedSave.reject(new Error('old project disk full'));
  await eventually(() => reads === 1);
  store.invalidateSettingsContext();
  const newLoad = store.loadSettings();
  await eventually(() => reads === 2);
  newProjectRead.resolve({ ...visible, language: 'de', uiTheme: 'new-project' });
  await newLoad;
  oldRecovery.resolve({ ...visible, language: 'hu', uiTheme: 'old-project' });
  await oldSave;

  assert.equal(visible.language, 'de');
  assert.equal(visible.uiTheme, 'new-project');
  delete globalThis.__settingsProjectRaceApp;
}

// Queue flushing observes a stable tail, not merely the save that happened to
// be pending when flushSettingsSaves was called.
{
  const first = deferred();
  const second = deferred();
  let saves = 0;
  globalThis.__settingsFlushApp = {
    SaveSettings() { return ++saves === 1 ? first.promise : second.promise; },
    GetSettings() { return Promise.resolve(null); },
    LogFrontend() { return Promise.resolve(); },
  };
  const store = await bundleStore('src/lib/stores/settings.ts', '__settingsFlushApp');
  void store.saveSettings({ language: 'hu' });
  await eventually(() => saves === 1);
  let flushed = false;
  const flushing = store.flushSettingsSaves().then(() => { flushed = true; });
  void store.saveSettings({ language: 'de' });
  first.resolve();
  await eventually(() => saves === 2);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(flushed, false);
  second.resolve();
  await flushing;
  assert.equal(flushed, true);
  delete globalThis.__settingsFlushApp;
}

// Invalidation happens before backend selection. If selection fails, the old
// project's backend may contain a mutation whose UI continuation was correctly
// discarded, so the failure path must reload that still-active project.
{
  const calls = { sessions: 0, settings: 0, lock: 0 };
  globalThis.window = { dispatchEvent() {} };
  globalThis.__projectRaceApp = new Proxy({
    SelectProject: async () => { throw new Error('selection refused'); },
    GetActiveProjectID: async () => 'old-project',
    GetSessions: async () => { calls.sessions++; return [{ id: 'same', name: 'backend truth' }]; },
    GetGroups: async () => [],
    GetSettings: async () => { calls.settings++; return null; },
    GetLockStatus: async () => { calls.lock++; return { locked: true, otherInstancePid: 0 }; },
  }, {
    get(target, key) {
      if (key in target) return target[key];
      return async () => undefined;
    },
  });
  const store = await bundleStore('src/lib/stores/projects.ts', '__projectRaceApp');
  store.activeProjectId.set('old-project');
  await assert.rejects(store.selectProject('missing-project'), /selection refused/);
  assert.deepEqual(calls, { sessions: 1, settings: 1, lock: 1 });
  delete globalThis.__projectRaceApp;
  delete globalThis.window;
}

// Lock status controls whether destructive project actions are enabled. A late
// response from the project that was left must not unlock the replacement.
{
  const oldLock = deferred();
  const newLock = deferred();
  let calls = 0;
  globalThis.__lockRaceApp = new Proxy({
    GetLockStatus() { return ++calls === 1 ? oldLock.promise : newLock.promise; },
  }, {
    get(target, key) {
      if (key in target) return target[key];
      return async () => undefined;
    },
  });
  const store = await bundleStore('src/lib/stores/projects.ts', '__lockRaceApp');
  let visiblePid = 0;
  store.otherInstancePID.subscribe((value) => { visiblePid = value; });
  store.activeProjectId.set('old');
  const stale = store.refreshLockStatus();
  store.activeProjectId.set('new');
  const current = store.refreshLockStatus();
  newLock.resolve({ locked: false, otherInstancePid: 4242 });
  await current;
  oldLock.resolve({ locked: true, otherInstancePid: 0 });
  await stale;
  assert.equal(visiblePid, 4242);
  delete globalThis.__lockRaceApp;
}

console.log('storeAsyncIdentity: ok');
console.error = originalConsoleError;
