// A function called from the markup re-runs only when its ARGUMENTS change.
// Reading a store inside it instead of taking it as a parameter hides the
// dependency from Svelte, and the markup keeps showing a stale value.
//
// This is what made the shortcut editor look broken after a reset: the store
// was updated and saved correctly — reopening the dialog proved it — but the
// row went on printing the keys it had rendered with, because keysFor(shortcut)
// took only the shortcut, and that object never changes.
//
// The rule is narrow: if a helper called from markup reads $store internally,
// its result cannot update. Pass the store in.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

function svelteFiles(dir, out = []) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) svelteFiles(path, out);
    else if (entry.endsWith('.svelte')) out.push(path);
  }
  return out;
}

/** Split a component into its <script> and its markup. */
function parts(source) {
  const end = source.lastIndexOf('</script>');
  return end < 0
    ? { script: '', markup: source }
    : { script: source.slice(0, end), markup: source.slice(end) };
}

/**
 * Cases that already existed when this test was written.
 *
 * They are real — each renders a value the markup cannot see change — but they
 * predate this check and fixing them is separate work with its own testing.
 * Listed so the check can guard against NEW ones today rather than waiting for
 * a sweep. Removing an entry after fixing it is the intended direction; adding
 * one is not.
 */
const KNOWN = new Set([
  'src/lib/components/Dashboard/ProjectDashboard.svelte: sessionActivity',
  'src/lib/components/Dashboard/ProjectDashboard.svelte: statusLabel',
  'src/lib/components/Dashboard/ProjectDashboard.svelte: tabsFor',
  'src/lib/components/Dialogs/SettingsDialog.svelte: formatDeleteAction',
  'src/lib/components/MainPanel/Diff.svelte: statusLabel',
  'src/lib/components/MainPanel/Diff.svelte: askRevertFile',
  'src/lib/components/MainPanel/TabBar.svelte: hasExtraArgs',
  'src/lib/components/MainPanel/TaskPanel.svelte: getTaskById',
  'src/lib/components/MainPanel/TaskPanel.svelte: formatRelativeDate',
]);

test('helpers called from markup do not read stores internally', () => {
  const offenders = [];

  for (const file of svelteFiles('src')) {
    const source = readFileSync(file, 'utf8');
    const { script, markup } = parts(source);

    // Plain (non-reactive, non-async) function declarations in the script.
    for (const match of script.matchAll(
      /(?<!\$:\s*)function\s+([a-zA-Z_]\w*)\s*\(([^)]*)\)[^{]*\{/g
    )) {
      const [, name, params] = match;

      // Its body, to the matching close brace.
      let depth = 0, i = match.index + match[0].length - 1, body = '';
      for (; i < script.length; i++) {
        const c = script[i];
        if (c === '{') depth++;
        else if (c === '}') { depth--; if (!depth) break; }
        body += c;
      }

      // Does the body read a store, without being handed one?
      const readsStore = /\$[a-zA-Z_]\w*\s*[.\[(]/.test(body);
      const takesStore = /\$/.test(params) || /Map<|bindings|store/i.test(params);
      if (!readsStore || takesStore) continue;

      // Is it called from the markup as a value (rather than an event handler,
      // where a stale read cannot be displayed)?
      const calledInMarkup = new RegExp(`[{\\s]${name}\\s*\\(`).test(markup) &&
        !new RegExp(`on:\\w+={\\s*\\(?[^}]*${name}\\s*\\(`).test(markup);
      if (calledInMarkup && !KNOWN.has(`${file}: ${name}`)) {
        offenders.push(`${file}: ${name}() reads a store but is called from markup`);
      }
    }
  }

  assert.deepEqual(offenders, [],
    'these render a value from a store the markup cannot see change — pass the ' +
    'store in as an argument so Svelte knows to re-run them');
});
