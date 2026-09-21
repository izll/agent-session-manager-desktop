import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const sheet = readFileSync(new URL('../src/style.css', import.meta.url), 'utf8');

function rule(selector) {
  const at = sheet.indexOf(selector + ' {');
  if (at < 0) return '';
  return sheet.slice(at, sheet.indexOf('}', at));
}

// Dialogs had a width but no height, and .dialog-content clips rather than
// scrolls — so one taller than the window was cut off, taking its own buttons
// with it. The new-session dialog hit this once it grew; every other long one
// could have.
//
// Measured in a browser: a max-height on its own clipped 375px and left the
// buttons unreachable. The rules below are the other half of it, which is why
// they live together in the shared sheet rather than in whichever dialog
// remembers to repeat them.
test('a dialog cannot grow past the window', () => {
  assert.match(rule('.dialog-content'), /max-height:/,
    'no height limit, so a long dialog runs off the screen');
});

test('the body scrolls instead of being clipped', () => {
  const body = rule('.dialog-content > form,\n.dialog-content > .dialog-body');
  assert.match(body, /overflow-y:\s*auto/,
    'nothing scrolls, so content past the height limit is simply cut off');

  // A flex child refuses to shrink below its content, so without this the
  // overflow moves back out to the dialog and the max-height does nothing.
  assert.match(body, /min-height:\s*0/,
    'a flex child will not shrink below its content without min-height: 0');

  assert.match(rule('.dialog-content'), /flex-direction:\s*column/,
    'the dialog must be a column for its body to be the part that gives way');
});

test('the buttons stay reachable however long the dialog gets', () => {
  const actions = rule('.dialog-content .dialog-actions,\n.dialog-content > .dialog-footer');
  assert.match(actions, /position:\s*sticky/,
    'the buttons scroll away with the body');
  assert.match(actions, /background:/,
    'sticky buttons need a background, or content shows through them');
});

// The point of putting it in the shared sheet: no dialog should have to
// remember. A local max-height is allowed — several set their own — but none
// should need to repeat the scrolling machinery.
test('no dialog repeats the scrolling machinery locally', () => {
  const dir = new URL('../src/lib/components/Dialogs/', import.meta.url);
  const repeats = [];
  for (const file of readdirSync(dir).filter(f => f.endsWith('.svelte'))) {
    const source = readFileSync(new URL(file, dir), 'utf8');
    // The tell-tale is a sticky .dialog-actions: that only exists to keep the
    // buttons reachable, which the shared sheet now does for everyone.
    const at = source.indexOf('.dialog-actions {');
    if (at < 0) continue;
    if (/position:\s*sticky/.test(source.slice(at, source.indexOf('}', at)))) {
      repeats.push(file);
    }
  }
  assert.deepEqual(repeats, [],
    'these repeat what the shared sheet already does: ' + repeats.join(', '));
});
