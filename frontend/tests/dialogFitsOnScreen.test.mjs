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

test('the buttons sit outside the scrolling body, not over it', () => {
  const actions = rule('.dialog-content > form ~ .dialog-actions,\n' +
    '.dialog-content > .dialog-body ~ .dialog-actions,\n' +
    '.dialog-content > form ~ .dialog-footer,\n' +
    '.dialog-content > .dialog-body ~ .dialog-footer');
  assert.match(actions, /flex-shrink:\s*0/,
    'the buttons can be squeezed out by a long form');

  // Sticky was the first attempt and it looked right: the buttons stayed put,
  // but a sticky element overlays the scroll area without reserving room, so
  // they covered the last field of the form. Measured — one field hidden.
  assert.doesNotMatch(actions, /position:\s*sticky/,
    'sticky buttons float over the form and hide its last field');
});

// The buttons only stay out of the way if they are a child of the dialog
// rather than of the scrolling form. A submit button moved out of its form
// needs form="id" to still submit it.
test('no dialog leaves its buttons inside the scrolling body', () => {
  const dir = new URL('../src/lib/components/Dialogs/', import.meta.url);
  const inside = [];
  for (const file of readdirSync(dir).filter(f => f.endsWith('.svelte'))) {
    const source = readFileSync(new URL(file, dir), 'utf8');
    const formAt = source.indexOf('<form');
    if (formAt < 0) continue;
    const formEnd = source.indexOf('</form>', formAt);
    if (formEnd < 0) continue;
    if (source.slice(formAt, formEnd).includes('class="dialog-actions"')) {
      inside.push(file);
    }
  }
  assert.deepEqual(inside, [],
    'these keep their buttons inside the scrolling form: ' + inside.join(', '));
});

// Moving the buttons out of the form breaks the submit unless they say which
// form they belong to.
test('a submit button outside its form still names it', () => {
  const dir = new URL('../src/lib/components/Dialogs/', import.meta.url);
  for (const file of readdirSync(dir).filter(f => f.endsWith('.svelte'))) {
    const source = readFileSync(new URL(file, dir), 'utf8');
    const formAt = source.indexOf('<form');
    if (formAt < 0) continue;
    const formEnd = source.indexOf('</form>', formAt);
    const afterForm = source.slice(formEnd);
    if (!afterForm.includes('type="submit"')) continue;
    assert.match(afterForm, /type="submit" form="[a-z-]+"/,
      `${file}: a submit button sits outside its form without naming it, ` +
      'so pressing it does nothing');
  }
});
