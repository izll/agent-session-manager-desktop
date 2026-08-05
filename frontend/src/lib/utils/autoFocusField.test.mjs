// A dialog opened to be typed into should land in its field.
//
// autoFocusDialog takes the first focusable child, and its selector includes
// buttons — so the task dialogs focused the dictate button in their header
// instead of the title box. That is right for a dialog you answer with Enter or
// Escape, and wrong for one you fill in, hence a second action rather than a
// change to the first.
//
// Mirrors TEXT_FIELD_SELECTOR and the fallback in utils/dialogActions.ts.
import { test } from 'node:test';
import assert from 'node:assert/strict';

/** Which element each action would focus, given a dialog's children. */
function firstFocusable(children) {
  return children.find((c) =>
    ['input', 'textarea', 'select', 'button'].includes(c.tag) && !c.disabled) || null;
}
function firstTextField(children) {
  return children.find((c) =>
    (c.tag === 'textarea' || (c.tag === 'input' && (!c.type || c.type === 'text'))) &&
    !c.disabled) || null;
}

const TASK_DIALOG = [
  { tag: 'button', name: 'dictate' },
  { tag: 'button', name: 'close' },
  { tag: 'input', type: 'text', name: 'title' },
  { tag: 'textarea', name: 'description' },
];

test('the old action lands on a header button — the reported bug', () => {
  assert.equal(firstFocusable(TASK_DIALOG).name, 'dictate');
});

test('the field action lands on the title', () => {
  assert.equal(firstTextField(TASK_DIALOG).name, 'title');
});

test('a textarea counts as a field', () => {
  // The PRD dialog opens straight into one.
  const dialog = [{ tag: 'button', name: 'close' }, { tag: 'textarea', name: 'prd' }];
  assert.equal(firstTextField(dialog).name, 'prd');
});

test('an input with no type attribute counts as text', () => {
  // The HTML default, and easy to write without thinking about it.
  const dialog = [{ tag: 'input', name: 'name' }];
  assert.equal(firstTextField(dialog).name, 'name');
});

test('a checkbox is not a field to type into', () => {
  const dialog = [{ tag: 'input', type: 'checkbox', name: 'research' },
                  { tag: 'input', type: 'text', name: 'title' }];
  assert.equal(firstTextField(dialog).name, 'title');
});

test('a disabled field is skipped', () => {
  const dialog = [{ tag: 'input', type: 'text', name: 'locked', disabled: true },
                  { tag: 'input', type: 'text', name: 'title' }];
  assert.equal(firstTextField(dialog).name, 'title');
});

test('a dialog with no fields falls back to the first focusable', () => {
  // The complexity report, which is read rather than filled in — focusing
  // nothing would leave Escape going to the terminal underneath.
  const dialog = [{ tag: 'button', name: 'close' }];
  assert.equal(firstTextField(dialog), null);
  assert.equal(firstFocusable(dialog).name, 'close');
});
