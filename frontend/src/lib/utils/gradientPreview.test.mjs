// The colour dialog previewed gradients with its own copy of the CSS builder,
// which differed from the sidebar's in the case that matters.
//
// A gradient name it did not recognise produced an empty string. The preview
// element still carried -webkit-text-fill-color: transparent, so the name was
// clipped against no background at all and vanished — an invisible session name
// rather than a wrong colour.
//
// Mirrors getGradientCSS in utils/rowColors.ts and getGradientStyle in
// SessionColorDialog.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

const gradients = {
  sunset: ['#ff9a00', '#ff2d55'],
  ocean: ['#00c6ff', '#0072ff'],
};

/** The shared helper: unknown names come back unchanged. */
function getGradientCSS(value) {
  const colors = gradients[value];
  return colors ? `linear-gradient(90deg, ${colors.join(', ')})` : value;
}

/** The dialog's wrapper: empty means "render this normally". */
function getGradientStyle(name) {
  const css = getGradientCSS(name);
  if (css === name) return '';
  return `background: ${css};`;
}

/** Whether the preview takes the gradient branch, which clips text. */
function usesGradientBranch(name) {
  const isGradient = name in gradients || getGradientStyle(name) !== '';
  return isGradient && getGradientStyle(name) !== '';
}

test('a known gradient renders as one', () => {
  assert.equal(getGradientStyle('sunset'), 'background: linear-gradient(90deg, #ff9a00, #ff2d55);');
  assert.ok(usesGradientBranch('sunset'));
});

test('an unknown gradient does not take the clipping branch', () => {
  // This is the bug: it did, with nothing to clip against, so the name
  // disappeared entirely.
  assert.equal(getGradientStyle('no-such-gradient'), '');
  assert.equal(usesGradientBranch('no-such-gradient'), false);
});

test('a plain colour is left alone', () => {
  assert.equal(getGradientStyle('#ff0000'), '');
});

test('the preview and the sidebar build the same CSS', () => {
  // They had separate copies; the preview is only useful if it shows what the
  // list will show.
  for (const name of Object.keys(gradients)) {
    assert.equal(getGradientStyle(name), `background: ${getGradientCSS(name)};`);
  }
});

test('every known gradient resolves to something paintable', () => {
  for (const name of Object.keys(gradients)) {
    const css = getGradientCSS(name);
    assert.ok(css.startsWith('linear-gradient('), `${name} produced ${css}`);
    assert.notEqual(css, name);
  }
});
