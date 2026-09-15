import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(
  new URL('../src/lib/utils/uiThemes.ts', import.meta.url), 'utf8');
const settingsSrc = readFileSync(
  new URL('../src/lib/stores/settings.ts', import.meta.url), 'utf8');
const cssSrc = readFileSync(
  new URL('../src/style.css', import.meta.url), 'utf8');
const dialogSrc = readFileSync(
  new URL('../src/lib/components/Dialogs/SettingsDialog.svelte', import.meta.url), 'utf8');
const appSrc = readFileSync(
  new URL('../src/App.svelte', import.meta.url), 'utf8');

const {
  UI_BACKGROUNDS, DEFAULT_UI_BACKGROUND, CUSTOM_UI_BACKGROUND,
  buildBackgroundFrom, getUIBackground, accentContrastOnBackground,
  UI_THEMES, DEFAULT_UI_THEME, getUITheme,
} = await import('../src/lib/utils/uiThemes.ts');

// The default has to reproduce exactly what was hard-coded before it was a
// setting, or every existing install changes appearance on upgrade.
test('the default background is the colours the app already had', () => {
  const midnight = getUIBackground(DEFAULT_UI_BACKGROUND);
  assert.equal(midnight.base, '#0d0d1a');
  assert.equal(midnight.surface, '#0a0a0f');
  assert.equal(midnight.raised, '#1a1a2e');
  assert.equal(midnight.sunken, '#0f0f1a');
});

// The layering is the whole point: a panel reads as a panel because it sits
// above the window behind it. Derived layers that collapse together, or invert,
// lose that.
test('every stock background keeps its layers apart and in order', () => {
  const lum = (hex) => {
    const n = parseInt(hex.slice(1), 16);
    const c = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
      const s = v / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
  };

  for (const bg of UI_BACKGROUNDS) {
    assert.ok(lum(bg.raised) > lum(bg.base),
      `${bg.id}: raised is not above base, so panels sink into the window`);
    assert.ok(lum(bg.base) > lum(bg.surface),
      `${bg.id}: content areas are not below the window`);
    assert.ok(lum(bg.raised) > lum(bg.sunken),
      `${bg.id}: the dialog gradient runs the wrong way`);
    assert.notEqual(bg.raised, bg.base, `${bg.id}: two layers are the same colour`);
  }
});

// A light background inverts which direction "above" is. Shading always
// towards white would run the raised layer off the top and flatten it.
test('a light background still separates its layers', () => {
  const pale = buildBackgroundFrom('pale', 'Pale', '#f2f2f5');
  const asInt = (hex) => parseInt(hex.slice(1), 16);
  assert.notEqual(pale.raised, pale.base, 'the raised layer collapsed into the base');
  assert.ok(asInt(pale.raised) < asInt(pale.base),
    'on a light background the raised layer has to darken, not brighten past white');
});

// Ordering alone let a six-fold overshoot through: shading a third of the way
// to white turned #141416 into a mid-grey #646465 panel, which is "above the
// base" and still wrong. The step has to be the one the default already uses.
test('the raised layer is a step above the base, not a leap', () => {
  const chan = (hex) => {
    const n = parseInt(hex.slice(1), 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  };
  const midnight = UI_BACKGROUNDS.find((b) => b.id === 'midnight');
  const reference = chan(midnight.raised)[0] - chan(midnight.base)[0];

  for (const bg of UI_BACKGROUNDS) {
    const lift = chan(bg.raised)[0] - chan(bg.base)[0];
    assert.ok(Math.abs(lift - reference) <= 6,
      `${bg.id}: panels sit ${lift} levels above the window, the default uses ` +
      `${reference} — at this distance they read as a different theme`);
  }
});

// Mixing towards white pulls the low channels up hardest, so a blue background
// comes back grey. The derived layers have to keep the colour they were given.
test('a derived layer keeps the hue of the colour it came from', () => {
  const chan = (hex) => {
    const n = parseInt(hex.slice(1), 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  };
  const spread = (hex) => {
    const c = chan(hex);
    return Math.max(...c) - Math.min(...c);
  };

  for (const bg of UI_BACKGROUNDS) {
    if (spread(bg.base) < 4) continue;   // a grey has no hue to lose
    assert.ok(spread(bg.raised) >= spread(bg.base) - 2,
      `${bg.id}: the raised layer washed out from a spread of ${spread(bg.base)} ` +
      `to ${spread(bg.raised)}, so a coloured background turns grey where it matters`);
  }
});

test('every layer publishes an rgb triple for the translucent tints', () => {
  for (const bg of UI_BACKGROUNDS) {
    for (const key of ['baseRgb', 'surfaceRgb', 'raisedRgb', 'sunkenRgb']) {
      assert.match(bg[key], /^\d{1,3}, \d{1,3}, \d{1,3}$/,
        `${bg.id}.${key} is not an "r, g, b" triple, so rgba() tints break`);
    }
  }
});

test('an unknown id falls back to the default rather than to nothing', () => {
  assert.equal(getUIBackground('no-such-background').id, DEFAULT_UI_BACKGROUND);
  // A custom id with no colour has nothing to derive from.
  assert.equal(getUIBackground(CUSTOM_UI_BACKGROUND).id, DEFAULT_UI_BACKGROUND);
});

test('a custom colour is accepted and derived from', () => {
  const bg = getUIBackground(CUSTOM_UI_BACKGROUND, '#101820');
  assert.equal(bg.base, '#101820');
  assert.notEqual(bg.raised, bg.base, 'the custom background has no layering');
});

// The warning said "this accent is too dark to see" while measuring against a
// colour the user might not be looking at any more.
test('the contrast warning follows the chosen background', () => {
  const accent = '#2b2b45';
  const onDark = accentContrastOnBackground(accent, '#0d0d1a');
  const onLight = accentContrastOnBackground(accent, '#f2f2f5');
  assert.ok(onLight > onDark,
    'the same accent scores the same on any background, so the background is ignored');

  // Called without one it still has to answer, for any caller not passing it.
  assert.ok(accentContrastOnBackground(accent) > 0);
});

test('the setting is stored and defaulted', () => {
  assert.match(settingsSrc, /uiBackground: string;/, 'the setting has no type');
  assert.match(settingsSrc, /uiBackgroundColor: string;/, 'the custom colour has no type');
  assert.match(settingsSrc, /uiBackground: 'midnight'/, 'a fresh install has no background');
});

// The variables have to exist in CSS as well: applyUIBackground overwrites them
// at runtime, but the stylesheet is what renders before the first write.
test('the stylesheet declares every variable as a fallback', () => {
  for (const v of ['--bg-base', '--bg-surface', '--bg-raised', '--bg-sunken',
                   '--bg-base-rgb', '--bg-surface-rgb', '--bg-raised-rgb', '--bg-sunken-rgb']) {
    assert.ok(cssSrc.includes(`${v}:`), `${v} is not declared, so it renders as nothing`);
  }
});

test('the background is applied at startup like the accent', () => {
  assert.match(appSrc, /applyUIBackground\(\$settings\.uiBackground/,
    'nothing applies the background, so the setting does nothing');
});

test('the dialog offers the background beside the accent', () => {
  assert.match(dialogSrc, /UI_BACKGROUNDS as bg/, 'the picker is missing');
  assert.match(dialogSrc, /pickCustomBackground/, 'a custom colour cannot be chosen');
  assert.match(dialogSrc, /accentContrastOnBackground\(customAccent, activeBackgroundBase\)/,
    'the contrast warning still ignores the chosen background');
});

// The colours are hard-coded in three files that cannot use a variable: the
// stylesheet's own fallbacks, the xterm theme object, and the pre-mixed gutter.
// Everywhere else a leftover literal is a spot that will not follow the setting.
test('no component still paints a hard-coded background', () => {
  const offenders = [];
  for (const [file, body] of [
    ['App.svelte', appSrc],
    ['SettingsDialog.svelte', dialogSrc],
  ]) {
    for (const m of body.matchAll(/background[^;:]*:\s*[^;]*#(0a0a0f|0d0d1a|1a1a2e|0f0f1a)/g)) {
      offenders.push(`${file}: ${m[0]}`);
    }
  }
  assert.deepEqual(offenders, [], `these still paint a fixed background:\n${offenders.join('\n')}`);
});

// Picking a custom colour leaves the id at 'custom' and changes only the hex.
// A reactive statement naming just the id never re-runs, and whatever it drives
// stays on the background it was built with — which is how the preview pane got
// stuck. Anything depending on the background has to name both.
test('nothing follows the background id alone', () => {
  const files = [
    ['App.svelte', appSrc],
    ['Preview.svelte', readFileSync(
      new URL('../src/lib/components/MainPanel/Preview.svelte', import.meta.url), 'utf8')],
  ];
  for (const [name, body] of files) {
    // To the end of the statement, not the end of the line: the call may be
    // wrapped, and a line-bounded match reads only half of it.
    for (const m of body.matchAll(/^[ \t]*\$:[\s\S]*?;/gm)) {
      const statement = m[0];
      if (!statement.includes('uiBackground')) continue;
      assert.ok(statement.includes('uiBackgroundColor'),
        `${name} has a reactive statement on the background id that never sees a ` +
        `custom colour change:\n${statement}`);
    }
  }
});

// The two grids sit one under the other in Settings, so the swatch in a given
// position of one reads as the partner of the swatch in the same position of
// the other. That only holds if the arrays are ordered to match: pair for pair,
// not merely both sorted by hue — sorting each independently put Wine under
// Violet (64 degrees apart) and Slate under Rose (127).
test('the two grids line up pair for pair', () => {
  const hue = (hex) => {
    const n = parseInt(hex.slice(1), 16);
    const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => v / 255);
    const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
    if (d === 0) return 0;
    const h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
    return ((h * 60) + 360) % 360;
  };
  const apart = (a, b) => {
    const d = Math.abs(a - b) % 360;
    return d > 180 ? 360 - d : d;
  };
  const greyish = (hex) => {
    const n = parseInt(hex.slice(1), 16);
    const c = [(n >> 16) & 255, (n >> 8) & 255, n & 255];
    return Math.max(...c) - Math.min(...c) < 40;
  };

  // The backgrounds may run one longer: Graphite is a neutral grey with no
  // accent to answer, kept because it is the only option for wanting no colour
  // in the window at all. Anything beyond that is an unpaired swatch.
  assert.ok(UI_BACKGROUNDS.length - UI_THEMES.length <= 1,
    `${UI_BACKGROUNDS.length - UI_THEMES.length} backgrounds have no accent ` +
    `beside them, so the positions no longer correspond`);
  assert.ok(UI_BACKGROUNDS.length >= UI_THEMES.length,
    'an accent has no background under it at all');

  for (let i = 0; i < UI_THEMES.length; i++) {
    const th = UI_THEMES[i], bg = UI_BACKGROUNDS[i];
    // A near-grey accent is paired for neutrality, not for hue.
    if (greyish(th.accent)) continue;
    const d = apart(hue(th.accent), hue(bg.base));
    assert.ok(d <= 35,
      `position ${i + 1} pairs the ${th.id} accent with the ${bg.id} background, ` +
      `${d.toFixed(0)} degrees apart — the two grids no longer read as pairs`);
  }
});

// An unpaired background is only defensible if it is the neutral one and it
// sits after the pairs. Anywhere else it shifts every pair below it by one.
test('an unpaired background is neutral and last', () => {
  if (UI_BACKGROUNDS.length === UI_THEMES.length) return;

  const extra = UI_BACKGROUNDS[UI_BACKGROUNDS.length - 1];
  const n = parseInt(extra.base.slice(1), 16);
  const c = [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  assert.ok(Math.max(...c) - Math.min(...c) < 12,
    `${extra.id} is the unpaired background but it is not neutral — a coloured ` +
    `swatch with no accent beside it reads as a pairing that went wrong`);
});

// Hue alone does not settle every pair: Blue and Slate are 2 degrees apart, and
// so are Ink and Graphite, so both arrangements look equally good by hue and
// the wrong one puts a saturated blue on a grey background and a grey accent on
// the bluest background there is. Saturation is what separates them.
test('a saturated accent gets the more saturated background of a close pair', () => {
  const sat = (hex) => {
    const n = parseInt(hex.slice(1), 16);
    const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => v / 255);
    const max = Math.max(r, g, b), min = Math.min(r, g, b);
    const l = (max + min) / 2;
    if (max === min) return 0;
    return l > 0.5 ? (max - min) / (2 - max - min) : (max - min) / (max + min);
  };

  const blue = UI_THEMES.findIndex((t) => t.id === 'blue');
  const slate = UI_THEMES.findIndex((t) => t.id === 'slate');
  assert.ok(blue >= 0 && slate >= 0, 'the blue or slate accent is gone');

  assert.ok(sat(UI_BACKGROUNDS[blue].base) > sat(UI_BACKGROUNDS[slate].base),
    `the blue accent sits on ${UI_BACKGROUNDS[blue].id} and the grey slate accent on ` +
    `${UI_BACKGROUNDS[slate].id} — the saturated background belongs under the ` +
    `saturated accent`);
});

// The arrays are ordered for the picker, so the first element is whatever the
// grid happens to start with. A fallback taking it would change meaning every
// time the swatches are reordered.
test('an unknown id falls back to the named default, not to the first swatch', () => {
  assert.equal(getUIBackground('no-such-background').id, DEFAULT_UI_BACKGROUND);
  assert.equal(getUITheme('no-such-theme').id, DEFAULT_UI_THEME);
  assert.notEqual(UI_THEMES[0].id, DEFAULT_UI_THEME,
    'the default is the first swatch again, so this no longer proves anything');
  assert.notEqual(UI_BACKGROUNDS[0].id, DEFAULT_UI_BACKGROUND,
    'the default is the first swatch again, so this no longer proves anything');
});
