// The sidebar's real stores, bundled with only the backend stubbed out, for
// tests that have to run the derivations rather than read their source.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

const root = new URL('../', import.meta.url);

export async function loadSidebarStores() {
  const bindings = readFileSync(new URL('wailsjs/go/main/App.d.ts', root), 'utf8');
  const appMethods = [...new Set([...bindings.matchAll(/export function ([A-Za-z0-9_]+)/g)].map(m => m[1]))];
  const result = await build({
    stdin: {
      contents: `
        export { get } from 'svelte/store';
        export * from './src/lib/stores/sidebarOrder.ts';
        export * from './src/lib/stores/sessions.ts';
        export { settings, saveSettings } from './src/lib/stores/settings.ts';
        export { lastActive } from './src/lib/stores/statusLines.ts';
      `,
      resolveDir: fileURLToPath(root),
      loader: 'ts',
    },
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'node',
    plugins: [{
      name: 'sidebar-backend-stub',
      setup(api) {
        api.onResolve({ filter: /wailsjs\/go\/main\/App$/ }, () => ({ path: 'app', namespace: 'stub' }));
        api.onResolve({ filter: /utils\/terminal$/ }, () => ({ path: 'terminal', namespace: 'stub' }));
        api.onLoad({ filter: /^app$/, namespace: 'stub' }, () => ({
          // Every backend call succeeds and returns nothing: saving a setting
          // then leaves the store as the save set it.
          contents: appMethods.map(n => `export const ${n} = async () => undefined;`).join('\n'),
          loader: 'js',
        }));
        api.onLoad({ filter: /^terminal$/, namespace: 'stub' }, () => ({
          contents: `export const defaultTerminalRenderer = () => 'dom';`,
          loader: 'js',
        }));
      },
    }],
  });
  return import(
    `data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}#${Math.random()}`);
}
