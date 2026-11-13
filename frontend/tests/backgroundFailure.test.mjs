import { test } from 'node:test';
import assert from 'node:assert/strict';
import { describeBackgroundFailure, joinBackgroundFailures } from '../src/lib/utils/backgroundFailure.ts';

const t = (key, params = {}) => `${params.name} failed (${params.code})`;

test('a failure names the command, its exit code and the last lines it printed', () => {
  const text = describeBackgroundFailure({ name: 'Build', exitCode: 2, output: 'one\ntwo\nthree\nfour\nfive\nsix' }, t);
  assert.equal(text, 'Build failed (2)\nthree\nfour\nfive\nsix');
});

test('a failure that printed nothing is only its name and code', () => {
  assert.equal(describeBackgroundFailure({ name: 'X', exitCode: 1, output: '  \n' }, t), 'X failed (1)');
});

// Two failures close together: the second must not hide the first.
test('a failure joins the one still shown', () => {
  assert.equal(joinBackgroundFailures('', 'A failed (1)'), 'A failed (1)');
  assert.equal(joinBackgroundFailures('A failed (1)', 'B failed (2)'), 'A failed (1)\n\nB failed (2)');
});
