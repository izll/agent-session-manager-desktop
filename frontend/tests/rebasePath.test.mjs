import test from 'node:test';
import assert from 'node:assert/strict';

// The diff can show a folder the session chose while the Files view shows the
// tab's; a file of the diff has to be found again from there, or not at all.

const { rebasePath } = await import('../src/lib/utils/rebasePath.ts');

test('a file under the Files view\'s folder is named from there', () => {
  assert.equal(rebasePath('src/a.ts', '/work/app', '/work'), 'app/src/a.ts');
  assert.equal(rebasePath('a.ts', '/work', '/work'), 'a.ts');
  assert.equal(rebasePath('deep/b', '/work/app/', '/work/'), 'app/deep/b');
  assert.equal(rebasePath('', '/work/app', '/work/app'), '');
  assert.equal(rebasePath('src', '/work/app', '/work/app/src'), '');
});

test('Windows separators are understood', () => {
  assert.equal(rebasePath('src\\a.ts', 'C:\\work\\app', 'C:\\work'), 'app/src/a.ts');
});

test('a file outside the Files view\'s folder has no name there', () => {
  assert.equal(rebasePath('a.ts', '/other', '/work'), null);
  // A sibling that merely starts with the same letters is not inside.
  assert.equal(rebasePath('a.ts', '/work-old', '/work'), null);
  assert.equal(rebasePath('a.ts', '/work', '/work/app'), null);
  assert.equal(rebasePath('a.ts', '', '/work'), null);
});
