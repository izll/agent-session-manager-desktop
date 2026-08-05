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
 * Exemptions. Empty, and meant to stay that way.
 *
 * The nine cases this check first reported have all been fixed rather than
 * listed here — one of them was a false positive (a handler, where reading the
 * store when the event fires is exactly right), which is why the detection
 * below subtracts event-directive calls.
 */
const KNOWN = new Set([]);

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

      // Only calls that RENDER something matter. A handler reads the store when
      // the event fires, which is exactly when its value is wanted, so those
      // are not stale — an earlier version of this test reported askRevertFile,
      // which is only ever an on:click, as a fault.
      //
      // So: count the call sites, subtract the ones inside an event directive,
      // and report only if something is left.
      const allCalls = [...markup.matchAll(new RegExp(`\\b${name}\\s*\\(`, 'g'))].length;
      const handlerCalls =
        [...markup.matchAll(new RegExp(`on:\\w+(\\|\\w+)*=\\{[^}]*?\\b${name}\\s*\\(`, 'g'))].length;
      const calledInMarkup = allCalls > handlerCalls;
      if (calledInMarkup && !KNOWN.has(`${file}: ${name}`)) {
        offenders.push(`${file}: ${name}() reads a store but is called from markup`);
      }
    }
  }

  assert.deepEqual(offenders, [],
    'these render a value from a store the markup cannot see change — pass the ' +
    'store in as an argument so Svelte knows to re-run them');
});
