import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const dialog = readFileSync(
  new URL('../src/lib/components/Dialogs/NewSessionDialog.svelte', import.meta.url), 'utf8');

function rule(selector) {
  const at = dialog.indexOf(selector + ' {');
  if (at < 0) return '';
  return dialog.slice(at, dialog.indexOf('}', at));
}

// The dialog had a width but no height. With the agent grid, the resume list
// and the options all showing, it grew past the window — taking the title off
// the top and the Create button off the bottom, so it could neither be
// finished nor closed.
//
// Measured in a browser with the component's own CSS: in a 600px window the
// dialog is 542px, header and actions both fully visible, and 2015px of form
// scrolls in the middle — including while scrolled to the very bottom.
test('the dialog cannot grow past the window', () => {
  const content = rule('  .dialog-content');
  assert.match(content, /max-height:/,
    'the dialog has no height limit, so a long form runs off the screen');
});

test('the header and the buttons stay put while the middle scrolls', () => {
  const form = rule('  .dialog-content > form');
  assert.match(form, /overflow-y:\s*auto/,
    'nothing scrolls, so the overflow falls out of the dialog instead');

  // Without this a flex child refuses to shrink below its content and the
  // overflow moves back out to the dialog, undoing the max-height.
  assert.match(form, /min-height:\s*0/,
    'a flex child will not shrink below its content without min-height: 0');

  const actions = rule('  .dialog-actions');
  assert.match(actions, /position:\s*sticky/,
    'the buttons scroll away with the form');
  assert.match(actions, /background:/,
    'sticky buttons need a background, or the form shows through them');
});
