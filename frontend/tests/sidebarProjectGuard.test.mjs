import assert from 'node:assert/strict';
import { build } from 'esbuild';

const root = new URL('../', import.meta.url);

const result = await build({
  entryPoints: [new URL('src/lib/stores/sidebarPolling.ts', root).pathname],
  bundle: true,
  write: false,
  format: 'esm',
  platform: 'node',
  plugins: [{
    name: 'sidebar-project-mocks',
    setup(api) {
      api.onResolve({ filter: /wailsjs\/runtime\/runtime$/ }, () => ({ path: 'runtime', namespace: 'mock' }));
      api.onLoad({ filter: /^runtime$/, namespace: 'mock' }, () => ({
        contents: `
          export const EventsOn = (_name, handler) => {
            globalThis.__sidebarHandler = handler;
            return () => {};
          };
          export const EventsOff = () => {};
        `,
        loader: 'js',
      }));
      api.onResolve({ filter: /\.\/projects$/ }, () => ({ path: 'projects', namespace: 'mock' }));
      api.onLoad({ filter: /^projects$/, namespace: 'mock' }, () => ({
        contents: `
          export const activeProjectId = {
            subscribe(handler) {
              handler(globalThis.__activeProjectId);
              return () => {};
            }
          };
        `,
        loader: 'js',
      }));
      api.onResolve({ filter: /\.\/activities$/ }, () => ({ path: 'activities', namespace: 'mock' }));
      api.onLoad({ filter: /^activities$/, namespace: 'mock' }, () => ({
        contents: `export const activities = { set(value) { globalThis.__activities = value; } };`,
        loader: 'js',
      }));
      api.onResolve({ filter: /\.\/statusLines$/ }, () => ({ path: 'status-lines', namespace: 'mock' }));
      api.onLoad({ filter: /^status-lines$/, namespace: 'mock' }, () => ({
        contents: `
          const sink = (name) => ({ set(value) { globalThis[name] = value; } });
          export const statusLines = sink('__statusLines');
          export const spinnerTexts = sink('__spinnerTexts');
          export const tabStatuses = sink('__tabStatuses');
        `,
        loader: 'js',
      }));
    },
  }],
});

globalThis.__activeProjectId = 'project-b';
const module = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}#${Date.now()}`);
module.startSidebarPolling();

assert.equal(typeof globalThis.__sidebarHandler, 'function');
globalThis.__sidebarHandler({
  projectId: 'project-a',
  activities: { shared: 'busy' },
  statusLines: { shared: 'wrong project' },
  spinnerTexts: {},
  tabStatuses: {},
});
assert.equal(globalThis.__activities, undefined, 'a late old-project event must be ignored');
assert.equal(globalThis.__statusLines, undefined, 'old-project status must not reach the active store');

globalThis.__sidebarHandler({
  projectId: 'project-b',
  activities: { shared: 'waiting' },
  statusLines: { shared: 'current project' },
  spinnerTexts: { shared: 'thinking' },
  tabStatuses: { shared: [] },
});
assert.deepEqual(globalThis.__activities, { shared: 'waiting' });
assert.deepEqual(globalThis.__statusLines, { shared: 'current project' });

module.stopSidebarPolling();
delete globalThis.__sidebarHandler;
delete globalThis.__activeProjectId;
delete globalThis.__activities;
delete globalThis.__statusLines;
delete globalThis.__spinnerTexts;
delete globalThis.__tabStatuses;

console.log('sidebarProjectGuard: ok');
