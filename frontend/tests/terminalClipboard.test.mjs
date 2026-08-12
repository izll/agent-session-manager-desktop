import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The terminal copies through the app, not through the browser.
 *
 * navigator.clipboard is refused in a WebKit webview in ways that depend on the
 * machine — and the refusal arrives as a rejected promise. Discarded, as it
 * was, the selection highlights, nothing reaches the clipboard, and nothing
 * anywhere says so. It worked on one machine and not another, with the same
 * build, the same setting and the same WebKit.
 *
 * ClipboardSetText is the Wails runtime's own, talking to the platform
 * directly. The diff and log views already use it; the terminal was the
 * exception.
 */
const source = readFileSync(
  new URL('../src/lib/utils/terminal.ts', import.meta.url),
  'utf8',
);

// Comments still describe what it used to do; only code counts.
const code = source.split('\n')
  .filter((line) => !/^\s*(\/\/|\*|\/\*)/.test(line))
  .join('\n');
assert.doesNotMatch(
  code,
  /navigator\.clipboard/,
  'the browser clipboard API is what failed silently in the webview',
);
assert.match(
  source,
  /import \{ ClipboardSetText \} from '\.\.\/\.\.\/\.\.\/wailsjs\/runtime\/runtime'/,
  'copying should go through the runtime, as the diff and log views do',
);

const copy = source.match(/const copySelection = \(([\s\S]*?)\n {2}\};/);
assert.ok(copy, 'copySelection is missing');
assert.match(copy[0], /ClipboardSetText\(text\)/, 'the selection goes to the runtime');

/**
 * And a refusal has to be recorded.
 *
 * ClipboardSetText reports failure two ways — a false return and a rejected
 * promise — and both were the shape the old code threw away. A clipboard that
 * quietly does nothing is indistinguishable from one that works until someone
 * tries to paste.
 */
assert.match(
  copy[0],
  /if \(!ok\) LogFrontend/,
  'a false return means the clipboard refused it, and that must be said',
);
assert.match(
  copy[0],
  /\.catch\(\(e\) => LogFrontend/,
  'a rejection must reach the log rather than being discarded',
);
assert.doesNotMatch(
  copy[0],
  /catch\(\(\) => \{ \/\* ignore \*\/ \}\)/,
  'that is the pattern which hid this for months',
);

// The copy still hangs off a real gesture. Driving it from the selection
// itself is what made a chatty pane hammer the clipboard and freeze the UI.
assert.match(
  source,
  /container\.addEventListener\('mouseup', onMouseUp, true\)/,
  'copying stays tied to mouseup, never to selection changes',
);
assert.doesNotMatch(
  code,
  /onSelectionChange/,
  'selection-driven copying is what froze the UI on a busy pane',
);

console.log('terminalClipboard: ok');
