import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';

const dialogDir = new URL('../src/lib/components/Dialogs/', import.meta.url);
const dialogs = readdirSync(dialogDir).filter(f => f.endsWith('.svelte'));

const terminalSrc = readFileSync(
  new URL('../src/lib/utils/terminal.ts', import.meta.url), 'utf8');
const claimSrc = readFileSync(
  new URL('../src/lib/utils/dialogKeys.ts', import.meta.url), 'utf8');

// The claim is a timing device, so run the real thing rather than reading it.
const { claimKeyForDialog, keyClaimedByDialog, resetDialogKeyClaim } =
  await import('../src/lib/utils/dialogKeys.ts');

test('there are dialogs to check', () => {
  assert.ok(dialogs.length > 20, `only found ${dialogs.length} dialogs`);
});

test('a claim is live immediately and does not last', async () => {
  resetDialogKeyClaim();
  assert.equal(keyClaimedByDialog(), false, 'claimed before anything happened');

  claimKeyForDialog();
  assert.equal(keyClaimedByDialog(), true,
    'the claim is not visible straight away, which is the instant that matters');

  // It has to outlast a render and the handlers after it...
  await new Promise(r => setTimeout(r, 60));
  assert.equal(keyClaimedByDialog(), true, 'the claim expired while the key was still travelling');

  // ...but not so long that the next real keypress is swallowed.
  await new Promise(r => setTimeout(r, 140));
  assert.equal(keyClaimedByDialog(), false, 'the claim outlived the keystroke it was for');
});

// Escape closes the dialog, and Svelte removes the overlay before the terminal's
// handler runs — so at the one moment that matters the DOM says no dialog is
// open. Checking only the DOM is what let Escape through into the pane.
test('the terminal does not rely on the overlay alone', () => {
  const at = terminalSrc.indexOf('attachCustomKeyEventHandler');
  assert.ok(at > 0, 'the terminal key handler is gone');
  const handler = terminalSrc.slice(at, at + 2000);

  const guard = handler.indexOf('dialog-overlay');
  assert.ok(guard > 0, 'the overlay check is gone');

  const line = handler.slice(handler.lastIndexOf('if (', guard), handler.indexOf('\n', guard));
  assert.match(line, /keyClaimedByDialog\(\)/,
    'the terminal still asks only the DOM, so a closing dialog leaks its key');
});

// Every dialog has to raise the claim, or its Escape is the one that leaks.
for (const file of dialogs) {
  const src = readFileSync(new URL(file, dialogDir), 'utf8');
  if (!src.includes('Escape')) continue;

  test(`${file} claims the key it handles`, () => {
    assert.match(src, /claimKeyForDialog\(\)/,
      'this dialog does not claim its keystroke, so Escape reaches the terminal');
  });

  test(`${file} stops the key it handles`, () => {
    assert.match(src, /stopPropagation\(\)/,
      'the key carries on to whatever else is listening');
  });
}

// The claim is for keyboard events. Raising it from a mouse handler would keep
// the terminal out for 150ms after an unrelated click — it happened once, on a
// dialog resize handler.
for (const file of dialogs) {
  const src = readFileSync(new URL(file, dialogDir), 'utf8');
  if (!src.includes('claimKeyForDialog')) continue;

  test(`${file} claims only from keyboard handlers`, () => {
    for (const match of src.matchAll(/claimKeyForDialog\(\)/g)) {
      // Search back to the enclosing function from the whole file: a window of
      // fixed size lands mid-function and picks up the signature above it.
      const before = src.slice(0, match.index);
      const fn = before.lastIndexOf('function ');
      assert.ok(fn >= 0, `no enclosing function for the claim in ${file}`);
      const signature = before.slice(fn, before.indexOf(')', fn) + 1);
      assert.ok(!/MouseEvent/.test(signature),
        `a mouse handler claims the keyboard in ${file}, locking the terminal ` +
        `out after a click: ${signature}`);
    }
  });
}

// The overlay check has the same blind spot everywhere it appears, not only in
// the terminal: a dialog closing on Escape removes the overlay before the key
// finishes travelling, so a shortcut guarded this way fires on the way out.
// Six places had it; missing one leaves a leak that looks identical.
test('every overlay check is paired with the claim', () => {
  const roots = ['../src/App.svelte',
    '../src/lib/utils/terminal.ts',
    '../src/lib/components/MainPanel/TabBar.svelte',
    '../src/lib/components/MainPanel/FileBrowser.svelte'];

  let checked = 0;
  for (const rel of roots) {
    const src = readFileSync(new URL(rel, import.meta.url), 'utf8');
    for (const match of src.matchAll(/document\.querySelector\('\.dialog-overlay'\)/g)) {
      checked++;
      // The claim has to sit in the same condition, so look to the end of the
      // statement rather than at a fixed number of characters.
      const rest = src.slice(match.index, src.indexOf(';', match.index) + 1);
      assert.match(rest, /keyClaimedByDialog\(\)/,
        `an overlay check in ${rel} has no claim beside it, so a closing ` +
        `dialog's key still gets through:\n${rest}`);
    }
  }
  assert.ok(checked >= 5, `only found ${checked} overlay checks; did they move?`);
});

test('the claim window is documented as deliberate', () => {
  assert.match(claimSrc, /CLAIM_MS/, 'the timing is no longer named');
  assert.match(claimSrc, /150/, 'the documented window changed without the comment');
});
