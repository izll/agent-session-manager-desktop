import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const colors = await import('../src/lib/utils/rowColors.ts');
const dialog = readFileSync(
  new URL('../src/lib/components/Dialogs/SessionColorDialog.svelte', import.meta.url), 'utf8');
const editor = readFileSync(
  new URL('../src/lib/components/Dialogs/CustomGradientDialog.svelte', import.meta.url), 'utf8');

// A custom gradient lives in the same colour field as everything else, so it
// has to read as a gradient to every place that only asks isGradient().
test('a custom gradient round-trips and is a gradient everywhere', () => {
  const value = colors.customGradient(['#ff0000', '#00FF00', '#0000ff']);
  assert.equal(value, 'gradient-custom:#FF0000,#00FF00,#0000FF');
  assert.ok(colors.isGradient(value), 'the sidebar would draw it as a flat colour');
  assert.deepEqual(colors.gradientStops(value), ['#FF0000', '#00FF00', '#0000FF']);
  assert.equal(colors.getGradientCSS(value),
    'linear-gradient(90deg, #FF0000, #00FF00, #0000FF)');
});

test('a custom gradient needs two to six colours', () => {
  assert.equal(colors.customGradient(['#FF0000']), '', 'one colour is not a gradient');
  const seven = Array(7).fill('#FF0000');
  assert.equal(colors.customGradient(seven), '');
  assert.ok(colors.customGradient(seven.slice(0, 6)));
});

// The value is written into a style attribute. A colours file can be imported,
// so a stored string must never reach the page as CSS of its own.
test('an unreadable gradient never reaches a style attribute as written', () => {
  for (const hostile of [
    'gradient-custom:red;position:fixed',
    'gradient-custom:#FF0000,#00FF00);background:url(http://x)',
    'gradient-custom:#FF0000',
    'gradient-custom:',
    'gradient-no-such-preset',
  ]) {
    const css = colors.getGradientCSS(hostile);
    assert.doesNotMatch(css, /[;()]\s*(position|background|url)/i, `${hostile} -> ${css}`);
    assert.ok(!css.includes(hostile), `${hostile} was passed through as it stands`);
    assert.match(css, /^linear-gradient\(90deg, (#[0-9A-F]{6}(, )?)+\)$/i,
      `${hostile} did not fall back to a safe gradient: ${css}`);
  }
  // Plain colours still pass through untouched.
  assert.equal(colors.getGradientCSS('#123456'), '#123456');
});

test('presets still resolve as before', () => {
  assert.match(colors.getGradientCSS('gradient-sunset'), /^linear-gradient\(90deg, #FF512F/);
});

// The editor is the last swatch of the grid, opening a window of its own.
test('"Custom" is the last swatch and opens the editor in its own window', () => {
  const grid = dialog.slice(dialog.indexOf('<div class="color-grid">'));
  const gridBody = grid.slice(0, grid.indexOf('<div class="dialog-footer">'));
  const each = gridBody.indexOf('{/each}');
  const custom = gridBody.indexOf('custom-btn');
  assert.ok(each > 0 && custom > each, 'the custom swatch is not after the preset swatches');
  assert.match(gridBody, /\{#if colorMode === 'text'\}\s*<button\s+class="color-btn custom-btn"/,
    'a gradient background is never drawn, so the swatch belongs to the text colour only');
  assert.match(gridBody, /on:click=\{\(\) => \(showCustom = true\)\}/);
  assert.match(dialog, /<CustomGradientDialog[\s\S]{0,200}on:apply=/,
    'the editor\'s result never reaches the selection');
  assert.doesNotMatch(dialog, /class="custom-gradient"/,
    'the editor is still embedded in the colour dialog');
});

// Two dialogs, one on top of the other: a key belongs to the top one.
test('Escape in the editor closes the editor only', () => {
  assert.match(editor, /use:portal/,
    'rendered inside the colour dialog, it would share its keydown handler');
  const at = editor.indexOf('function handleKeydown');
  const body = editor.slice(at, editor.indexOf('\n  }\n', at));
  assert.match(body, /stopPropagation\(\)/, 'the key reaches the dialog underneath too');
  assert.match(body, /claimKeyForDialog\(\)/, 'Escape can leak into the terminal behind');
});

// The editor took the focus; without handing it back, the colour dialog's own
// keys did nothing until it was clicked.
test('closing the editor gives the focus back to the colour dialog', () => {
  assert.match(dialog, /bind:this=\{overlayEl\}/);
  assert.match(dialog, /function customClosed\(\)[\s\S]{0,300}overlayEl\?\.focus\(\)/);
});

// The editor sits inside the colour dialog's {#if}. Closing the dialog took it
// off screen without closing it, so it opened again by itself the next time a
// colour dialog came up — for whatever row that was.
test('the editor never reopens by itself', () => {
  const closed = dialog.slice(dialog.indexOf('} else if (!show) {'));
  assert.match(closed.slice(0, closed.indexOf('\n    }\n')), /showCustom = false;/,
    'closing the colour dialog leaves the editor marked open');
  const opened = dialog.slice(dialog.indexOf('if (show && target && !targetCaptured) {'));
  assert.match(opened.slice(0, opened.indexOf('} else if')), /showCustom = false;/,
    'a new opening inherits the editor from the last one');
});

// Every gradient resolves to something paintable now — an unreadable one is
// grey — so the text style can no longer come back empty for one.
test('the preview does not guard against a gradient style that cannot be empty', () => {
  assert.doesNotMatch(dialog, /isGradient\(selectedColor\) && getGradientTextStyle/);
  const doc = dialog.slice(0, dialog.indexOf('function getGradientTextStyle'));
  assert.match(doc.slice(doc.lastIndexOf('/**')), /The preview's gradient/,
    'the preview\'s explanation sits on the wrong function');
});

// A half-typed hex was dropped silently: the field said "#12" while the stop
// kept its old colour, and Apply applied that old colour. What is applied has
// to be what the fields say.
test('a field that is not a colour is marked and holds Apply back', () => {
  assert.match(editor, /class="hex"[\s\S]{0,120}value=\{texts\[index\]\}/,
    'the field shows the stop, not what was typed, so the two can disagree unseen');
  assert.match(editor, /class:invalid=\{invalid\[index\]\}/, 'an unreadable field looks like any other');
  assert.match(editor, /aria-invalid=\{invalid\[index\]\}/);

  const typed = editor.slice(editor.indexOf('function typeStop'));
  const body = typed.slice(0, typed.indexOf('\n  }\n'));
  assert.ok(body.indexOf('texts[index] = input') >= 0 &&
    body.indexOf('texts[index] = input') < body.indexOf('return'),
    'a value that is not a colour is lost before it is recorded');

  assert.match(editor, /\$: invalid = texts\.map\(/);
  assert.match(editor, /disabled=\{!value \|\| anyInvalid\}/, 'Apply is offered over an unreadable field');
  const apply = editor.slice(editor.indexOf('function apply()'));
  assert.match(apply.slice(0, 80), /if \(!value \|\| anyInvalid\) return;/,
    'Enter still applies over an unreadable field');
});

test('Enter is left to a focused button, by the shared rule', () => {
  const at = editor.indexOf('function handleKeydown');
  const body = editor.slice(at, editor.indexOf('\n  }\n', at));
  assert.match(body, /e\.key === 'Enter' && !dialogEnterBelongsToControl\(e\)/);
  assert.doesNotMatch(body, /instanceof HTMLButtonElement/, 'a copy of the rule is kept by hand');
});

test('every new string is translated', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const keys = ['color.customGradient', 'color.custom', 'color.customGradientAdd',
    'color.customGradientRemove', 'color.customGradientStop', 'color.customGradientInvalid'];
  for (const name of readdirSync(dir).filter((n) => n.endsWith('.json'))) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of keys) assert.ok(strings[key]?.trim(), `${name} has no ${key}`);
    assert.match(strings['color.customGradientStop'], /\{n\}/, `${name} lost {n}`);
  }
});
