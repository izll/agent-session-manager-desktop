import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

const {
  NOTES_BACKGROUNDS, NOTES_TEXT_COLORS, DEFAULT_NOTES_TEXT, CUSTOM_NOTES_COLOR, MIN_NOTES_CONTRAST,
  parseHex, normaliseHex, contrastRatio, readableTextOn, isLightBackground,
  resolveNotesBackground, resolveNotesText, effectiveNotesText, notesColorVars, notesColorStyle,
  defaultNotesBackgroundOver,
} = await import('../src/lib/utils/notesColors.ts');

const luminance = (hex) => {
  const [r, g, b] = parseHex(hex).map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

test('hex colours are read in either length, with or without the hash', () => {
  assert.deepEqual(parseHex('#f4ecd8'), [244, 236, 216]);
  assert.deepEqual(parseHex('F4ECD8'), [244, 236, 216]);
  assert.deepEqual(parseHex('#abc'), [170, 187, 204]);
  assert.deepEqual(parseHex('  #000000 '), [0, 0, 0]);
  assert.equal(normaliseHex('#ABC'), '#aabbcc');
  for (const bad of ['', '#12', '#12345', '#gggggg', 'red', 'rgb(0,0,0)', null, undefined, 42]) {
    assert.equal(parseHex(bad), null, `${String(bad)} was read as a colour`);
    assert.equal(normaliseHex(bad), null);
  }
});

test('contrast is the WCAG ratio', () => {
  assert.equal(Math.round(contrastRatio('#000000', '#ffffff') * 10) / 10, 21);
  assert.equal(contrastRatio('#777777', '#777777'), 1);
  assert.equal(contrastRatio('#ffffff', '#000000'), contrastRatio('#000000', '#ffffff'));
  assert.equal(contrastRatio('nope', '#000000'), 1);
});

// The point of Auto: dark text on a light page, light text on a dark one,
// and readable either way.
test('a light background gets dark text and a dark one light text', () => {
  for (const bg of ['#ffffff', '#f4ecd8', '#f7f7f8', '#fff8dc', '#d0d0d0']) {
    const text = readableTextOn(bg);
    assert.ok(luminance(text) < luminance(bg), `${bg} got the lighter text ${text}`);
    assert.ok(luminance(text) < 0.1, `${bg} got ${text}, which is not dark`);
    assert.ok(isLightBackground(bg), `${bg} is not counted as light`);
  }
  for (const bg of ['#000000', '#0a0a0f', '#2a2c31', '#1e3a8a', '#4b1d1d']) {
    const text = readableTextOn(bg);
    assert.ok(luminance(text) > luminance(bg), `${bg} got the darker text ${text}`);
    assert.ok(luminance(text) > 0.6, `${bg} got ${text}, which is not light`);
    assert.ok(!isLightBackground(bg), `${bg} is counted as light`);
  }
});

test('Auto reads at 4.5:1 or better on any background', () => {
  for (let v = 0; v <= 255; v += 15) {
    for (const bg of [[v, v, v], [v, 0, 0], [0, v, 0], [0, 0, v], [v, v, 0], [255, v, 255 - v]]) {
      const hex = '#' + bg.map((n) => n.toString(16).padStart(2, '0')).join('');
      const ratio = contrastRatio(readableTextOn(hex), hex);
      assert.ok(ratio >= 4.5, `${hex}: Auto text is only ${ratio.toFixed(2)}:1`);
    }
  }
  assert.equal(readableTextOn('not a colour'), DEFAULT_NOTES_TEXT);
});

test('every stock background reads with its Auto text, and the stock texts are colours', () => {
  for (const bg of NOTES_BACKGROUNDS) {
    assert.ok(contrastRatio(readableTextOn(bg.hex), bg.hex) >= 7, `${bg.id} reads poorly`);
  }
  for (const c of [...NOTES_BACKGROUNDS, ...NOTES_TEXT_COLORS]) {
    assert.equal(normaliseHex(c.hex), c.hex, `${c.id} is not a #rrggbb colour`);
  }
  // The paper preset is a warm light page, the one users asked for.
  const paper = NOTES_BACKGROUNDS.find((b) => b.id === 'paper');
  assert.ok(paper && isLightBackground(paper.hex));
  assert.ok(MIN_NOTES_CONTRAST >= 3);
});

// An install that never set anything looks exactly as before: no variables,
// so the stylesheet's own colours apply.
test('the default background with Auto text sets nothing', () => {
  assert.deepEqual(notesColorVars('', '', '', ''), {});
  assert.deepEqual(notesColorVars(undefined, undefined, undefined, undefined), {});
  assert.deepEqual(notesColorVars('no-such-preset', '', '', ''), {});
  // "custom" with no usable colour is the default too, not a blank page.
  assert.deepEqual(notesColorVars(CUSTOM_NOTES_COLOR, 'garbage', CUSTOM_NOTES_COLOR, ''), {});
  assert.equal(notesColorStyle({}), '');
  assert.equal(effectiveNotesText('', '', '', ''), DEFAULT_NOTES_TEXT);
});

test('a preset or custom background brings its Auto text with it', () => {
  const paper = notesColorVars('paper', '', '', '');
  assert.equal(paper['--notes-bg'], '#f4ecd8');
  assert.equal(paper['--notes-fg'], readableTextOn('#f4ecd8'));
  assert.ok(luminance(paper['--notes-fg']) < 0.1);
  assert.match(paper['--notes-placeholder'], /^rgba\(/);
  assert.equal(paper['--notes-border'], 'rgba(0, 0, 0, 0.14)');

  const custom = notesColorVars(CUSTOM_NOTES_COLOR, '#123', '', '');
  assert.equal(custom['--notes-bg'], '#112233');
  assert.ok(luminance(custom['--notes-fg']) > 0.6);
  assert.equal(custom['--notes-border'], 'rgba(255, 255, 255, 0.1)');
  assert.equal(resolveNotesBackground('dim', ''), '#2a2c31');
  assert.equal(resolveNotesBackground('', '#ffffff'), null);
});

test('a chosen text colour wins over Auto, on any background', () => {
  const ink = notesColorVars('', '', 'ink', '');
  assert.equal(ink['--notes-fg'], '#1f2937');
  assert.equal(ink['--notes-bg'], undefined, 'the default background was replaced');
  assert.equal(notesColorVars('paper', '', CUSTOM_NOTES_COLOR, '#AA0000')['--notes-fg'], '#aa0000');
  assert.equal(resolveNotesText('', '#aa0000'), null);
  assert.equal(effectiveNotesText('paper', '', 'white', ''), '#f4f4f5');
  // White on paper is the case the warning is for.
  assert.ok(contrastRatio('#f4f4f5', '#f4ecd8') < MIN_NOTES_CONTRAST);
  assert.equal(notesColorStyle({ '--notes-fg': '#fff', '--notes-bg': '#000' }),
    '--notes-fg: #fff; --notes-bg: #000');
});

test('the default background is measured as it shows: a wash over the surface', () => {
  assert.equal(defaultNotesBackgroundOver('#0a0a0f'), '#08080c');
  assert.equal(defaultNotesBackgroundOver('not a colour'), '#08080c');
});

// The view draws from the variables, with today's colours as the fallbacks.
test('the notes view reads the colours from the settings and keeps the old look as fallback', () => {
  const notes = read('../src/lib/components/MainPanel/Notes.svelte');
  assert.match(notes, /notesColorVars\(\$settings\.notesBackground, \$settings\.notesBackgroundColor,\s*\$settings\.notesText, \$settings\.notesTextColor\)/);
  assert.match(notes, /background: var\(--notes-bg, rgba\(0, 0, 0, 0\.2\)\)/);
  assert.match(notes, /color: var\(--notes-fg, white\)/);
  assert.match(notes, /color: var\(--notes-placeholder, #4b5563\)/);
  const goSettings = read('../../app.go');
  for (const field of ['notesBackground', 'notesBackgroundColor', 'notesText', 'notesTextColor']) {
    assert.match(goSettings, new RegExp(`json:"${field}"`), `${field} is not passed through the backend`);
  }
});
