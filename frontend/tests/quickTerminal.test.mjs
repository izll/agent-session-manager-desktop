import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The quick terminal dialog asks one question and gets out of the way.
 *
 * The full new-tab dialog asks which agent, with what arguments, in which
 * directory — worth answering when starting an agent, all of it in the way when
 * what you want is a shell.
 */

const dialog = readFileSync(
  new URL('../src/lib/components/Dialogs/QuickTerminalDialog.svelte', import.meta.url),
  'utf8',
);
const shortcuts = readFileSync(
  new URL('../src/lib/utils/shortcuts.ts', import.meta.url),
  'utf8',
);
const tabBar = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url),
  'utf8',
);

// One field. Anything else and it is the dialog it exists to avoid.
const inputs = dialog.match(/<input\b/g) ?? [];
assert.equal(inputs.length, 1, 'the quick dialog must ask for the name and nothing else');
assert.doesNotMatch(dialog, /<select\b/, 'no agent picker — this opens a terminal');

// Filled in AND selected. Keeping the name costs Enter, replacing it costs
// typing; a selection is what makes both true at once. The shared
// autoFocusField action deliberately does the opposite (cursor at the end),
// which is right for dialogs opening on text you came to amend, wrong here.
assert.match(dialog, /inputEl\?\.select\(\)/, 'the suggested name must arrive selected');
// Checked against the code rather than the whole file: the comment above it
// names autoFocusField to explain why it is not used, and matching prose is
// how a test starts failing for being right.
const dialogCode = dialog.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
assert.doesNotMatch(dialogCode, /use:autoFocusField/, 'that action puts the cursor at the end instead of selecting');

// Enter submits. Without this the dialog is a form to tab through, and the
// whole point was one keystroke.
assert.match(
  dialog,
  /e\.key === 'Enter'[^}]*handleSubmit\(\)/s,
  'Enter must create the tab',
);

// And a button does the same. Enter is the fast path and the hint says so, but
// a dialog with no button reads as unfinished, and someone will look for one
// before trusting the keyboard.
assert.match(
  dialog,
  /class="btn-primary"[^>]*on:click=\{handleSubmit\}/,
  'the confirm button must run the same action as Enter',
);

// A terminal, not an agent, with the session's own defaults.
assert.match(
  dialog,
  /App\.CreateTab\(sessionId, false, 'terminal', trimmed, '', ''\)/,
  'it must create a plain terminal tab with no extra arguments',
);

// Bound, and rebindable like every other shortcut rather than hard-coded.
assert.match(shortcuts, /id: 'tab\.newTerminal'/, 'the shortcut must be a declared action');
assert.match(
  shortcuts,
  /id: 'tab\.newTerminal',[\s\S]{0,320}?defaults: \[\{ key: 't', ctrl: true, shift: true \}\]/,
  'Ctrl+Shift+T by default',
);
assert.match(
  tabBar,
  /matchesShortcut\(e, 'tab\.newTerminal'\)/,
  'the handler must match the binding, not a hard-coded key',
);

// Ctrl+T and Ctrl+Shift+T are separate actions. eventMatches compares shift
// exactly, so neither swallows the other — but they must stay distinct ids.
assert.match(tabBar, /matchesShortcut\(e, 'tab\.new'\)/, 'the full dialog keeps its own binding');

console.log('quickTerminal: ok');
