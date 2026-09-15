/**
 * Interface accent colours.
 *
 * Every purple in the app is driven from the CSS variables in style.css, so a
 * theme is just six values. `--accent-rgb` feeds the many rgba() tints;
 * the named steps cover solid fills, hovers and text.
 *
 * This is only the accent — backgrounds and greys stay as they are, which is
 * what keeps the contrast right without hand-checking every screen.
 */

export interface UITheme {
  id: string;
  /** Shown in Settings; not translated, these read as colour names. */
  name: string;
  /** "r, g, b" — the base accent, used for every translucent tint. */
  rgb: string;
  accent: string;
  light: string;
  lighter: string;
  pale: string;
  dark: string;
  /**
   * Text drawn ON a solid accent fill. White fails on the brighter accents —
   * measured, white on Amber is 2.15:1 — so those use a dark ink instead.
   */
  onAccent: string;
}

/**
 * The interface background, as four layers rather than one colour.
 *
 * "The app's base colour" is not a single value: the surfaces sit at different
 * depths and the difference between them is what makes a panel read as a panel.
 * Flattening them to one colour loses the layering; changing only the outermost
 * one leaves the panels black against whatever was chosen. So a background is
 * picked as one colour and the other three are derived from it, the way the
 * accent's lighter and darker steps are.
 *
 * base    — the window behind everything.
 * surface — content areas: terminals, diffs, the file tree, the dashboard.
 * raised  — things that sit on top: dialog headers, dropdowns, tooltips.
 * sunken  — the far end of the dialog gradients, a step below raised.
 */
export interface UIBackground {
  id: string;
  name: string;
  base: string;
  surface: string;
  raised: string;
  sunken: string;
  /** "r, g, b" of each layer, for the translucent tints written as rgba(). */
  baseRgb: string;
  surfaceRgb: string;
  raisedRgb: string;
  sunkenRgb: string;
}

/**
 * The stock backgrounds. The first reproduces the colours that were hard-coded
 * before this was a setting, so the default look is unchanged.
 */
export const UI_BACKGROUNDS: UIBackground[] = [
  // Ordered to match UI_THEMES below, pair for pair: the two grids sit one
  // under the other in Settings, and a swatch in the same position is the
  // background that goes with that accent.
  //
  // Umber, Petrol and Marine exist because three accents had no background
  // within reach — measured, Amber's nearest was 75 degrees away, Cyan's 34,
  // where every other accent had one inside 30. Petrol was then moved from 189
  // to 175 so that it answers Teal and leaves Cyan to Marine: at 189 the two
  // cold backgrounds were three degrees apart and read as the same swatch
  // twice.
  //
  // Slate is paired with the Slate accent by name as well as by hue. It is the
  // one pair where a reader would notice the mismatch without measuring
  // anything.
  buildBackgroundFrom('umber', 'Umber', '#151009'),
  buildBackgroundFrom('forest', 'Forest', '#0c1410'),
  buildBackgroundFrom('petrol', 'Petrol', '#091514'),
  buildBackgroundFrom('marine', 'Marine', '#091518'),
  buildBackgroundFrom('slate', 'Slate', '#111318'),
  buildBackgroundFrom('ink', 'Ink', '#0b1020'),
  // Midnight names all three of its layers rather than deriving them: these are
  // the colours the app used before the background was a setting, and an
  // upgrade must not change how it looks.
  buildBackgroundFrom('midnight', 'Midnight', '#0d0d1a', {
    surface: '#0a0a0f',
    raised: '#1a1a2e',
    sunken: '#0f0f1a',
  }),
  buildBackgroundFrom('wine', 'Wine', '#150d12'),
  // Graphite has no accent to pair with, and sits last for that reason: it is
  // the only neutral grey, which is worth keeping for anyone who wants no
  // colour in the window at all. Dropping it would have made the grids the
  // same length and taken the one option that answers that.
  buildBackgroundFrom('graphite', 'Graphite', '#141416'),
];

export const DEFAULT_UI_BACKGROUND = 'midnight';
export const CUSTOM_UI_BACKGROUND = 'custom';

export const UI_THEMES: UITheme[] = [
  // Ordered to match UI_BACKGROUNDS above — see the note there.
  {
    id: 'amber',
    name: 'Amber',
    rgb: '245, 158, 11',
    accent: '#f59e0b',
    light: '#fbbf24',
    lighter: '#fcd34d',
    pale: '#fef3c7',
    dark: '#d97706',
    onAccent: '#451a03',
  },
  {
    id: 'green',
    name: 'Green',
    rgb: '34, 197, 94',
    accent: '#22c55e',
    light: '#4ade80',
    lighter: '#86efac',
    pale: '#dcfce7',
    dark: '#16a34a',
    onAccent: '#052e16',
  },
  {
    id: 'teal',
    name: 'Teal',
    rgb: '20, 184, 166',
    accent: '#14b8a6',
    light: '#2dd4bf',
    lighter: '#5eead4',
    pale: '#ccfbf1',
    dark: '#0d9488',
    onAccent: '#042f2e',
  },
  {
    id: 'cyan',
    name: 'Cyan',
    rgb: '6, 182, 212',
    accent: '#06b6d4',
    light: '#22d3ee',
    lighter: '#67e8f9',
    pale: '#cffafe',
    dark: '#0891b2',
    onAccent: '#083344',
  },
  {
    id: 'slate',
    name: 'Slate',
    rgb: '100, 116, 139',
    accent: '#64748b',
    light: '#94a3b8',
    lighter: '#cbd5e1',
    pale: '#e2e8f0',
    dark: '#475569',
    onAccent: '#ffffff',
  },
  {
    id: 'blue',
    name: 'Blue',
    rgb: '59, 130, 246',
    accent: '#3b82f6',
    light: '#60a5fa',
    lighter: '#93c5fd',
    pale: '#dbeafe',
    dark: '#2563eb',
    onAccent: '#ffffff',
  },
  {
    id: 'violet',
    name: 'Violet',
    rgb: '139, 92, 246',
    accent: '#8b5cf6',
    light: '#a78bfa',
    lighter: '#c4b5fd',
    pale: '#ddd6fe',
    dark: '#7c3aed',
    onAccent: '#ffffff',
  },
  {
    id: 'rose',
    name: 'Rose',
    rgb: '244, 63, 94',
    accent: '#f43f5e',
    light: '#fb7185',
    lighter: '#fda4af',
    pale: '#ffe4e6',
    dark: '#e11d48',
    onAccent: '#ffffff',
  },
];

export const DEFAULT_UI_THEME = 'violet';

export function getUITheme(id: string, customHex?: string): UITheme {
  if (id === CUSTOM_UI_THEME && customHex) {
    const built = buildCustomTheme(customHex);
    if (built) return built;
  }
  // Named rather than UI_THEMES[0]: the array is ordered for the picker, so
  // taking its first element would tie the fallback to wherever the grid
  // happens to start — reordering the swatches would silently change what an
  // unknown id resolves to.
  return UI_THEMES.find((t) => t.id === id)
    || UI_THEMES.find((t) => t.id === DEFAULT_UI_THEME)
    || UI_THEMES[0];
}

/**
 * Write a theme into the document. Applied to the root element so every
 * component picks it up through the variables, with no re-render needed.
 */
export function applyUITheme(id: string, customHex?: string): void {
  const t = getUITheme(id, customHex);
  const s = document.documentElement.style;
  s.setProperty('--accent-rgb', t.rgb);
  s.setProperty('--accent', t.accent);
  s.setProperty('--accent-light', t.light);
  s.setProperty('--accent-lighter', t.lighter);
  s.setProperty('--accent-pale', t.pale);
  s.setProperty('--accent-dark', t.dark);
  s.setProperty('--accent-ink', t.onAccent);
}

// --- Custom accent --------------------------------------------------------

export const CUSTOM_UI_THEME = 'custom';

function clamp01(n: number): number {
  return Math.min(1, Math.max(0, n));
}

function hexToRgb(hex: string): [number, number, number] | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return null;
  const v = m[1];
  return [
    parseInt(v.slice(0, 2), 16),
    parseInt(v.slice(2, 4), 16),
    parseInt(v.slice(4, 6), 16),
  ];
}

function rgbToHex(r: number, g: number, b: number): string {
  const h = (n: number) => Math.round(clamp01(n / 255) * 255).toString(16).padStart(2, '0');
  return `#${h(r)}${h(g)}${h(b)}`;
}

/** Mix a colour towards white (t > 0) or black (t < 0). */
function shade(rgb: [number, number, number], t: number): string {
  const target = t > 0 ? 255 : 0;
  const k = Math.abs(t);
  return rgbToHex(
    rgb[0] + (target - rgb[0]) * k,
    rgb[1] + (target - rgb[1]) * k,
    rgb[2] + (target - rgb[2]) * k,
  );
}

/**
 * Build a background from one colour, deriving the layers it does not name.
 *
 * The stock set was measured off the colours that were hard-coded before:
 * surface sits below base, raised well above it, sunken between the two. Those
 * offsets are what the derived ones reproduce, so a chosen colour lands in the
 * same relationship its neighbours had.
 *
 * A very light colour would inverts these steps — raised would run off the top
 * — so the shades are taken towards white or black by luminance rather than
 * always in one direction.
 */
export function buildBackgroundFrom(
  id: string,
  name: string,
  base: string,
  fixed?: { surface?: string; raised?: string; sunken?: string },
): UIBackground {
  const rgb = hexToRgb(base) || [13, 13, 26];
  const light = luminance(rgb) > 0.4;

  /**
   * Lift a colour by a fixed number of levels rather than by a fraction of the
   * distance to white.
   *
   * Measured off the colours the app already used: #0d0d1a to #1a1a2e is 13
   * levels up, which as a fraction of the remaining distance to white is about
   * 0.05 — shading by a third, as this first did, overshot it six-fold and
   * turned every panel mid-grey. It also washed the hue out, because a mix
   * towards white pulls the low channels up hardest: the blue background came
   * back grey.
   *
   * Adding a constant keeps both the step and the hue: the channels move
   * together, so the colour stays the colour it was, a little lighter.
   */
  const step = (levels: number): string => {
    const dir = light ? -1 : 1;
    const m = (v: number) => Math.round(Math.min(255, Math.max(0, v + levels * dir)));
    return rgbToHex(m(rgb[0]), m(rgb[1]), m(rgb[2]));
  };

  // The offsets between the four layers of the default, which is what every
  // derived background reproduces: content sits a little below the window,
  // panels a clear step above it, and the dialog gradient ends between them.
  const surface = fixed?.surface ?? step(-4);
  const raised = fixed?.raised ?? step(14);
  const sunken = fixed?.sunken ?? step(3);
  const triple = (hex: string) => {
    const c = hexToRgb(hex) || rgb;
    return `${c[0]}, ${c[1]}, ${c[2]}`;
  };

  return {
    id,
    name,
    base: rgbToHex(rgb[0], rgb[1], rgb[2]),
    surface,
    raised,
    sunken,
    baseRgb: triple(base),
    surfaceRgb: triple(surface),
    raisedRgb: triple(raised),
    sunkenRgb: triple(sunken),
  };
}

/** Resolve a background id, falling back to the default rather than to nothing. */
export function getUIBackground(id: string, customHex?: string): UIBackground {
  if (id === CUSTOM_UI_BACKGROUND && customHex) {
    const rgb = hexToRgb(customHex);
    if (rgb) return buildBackgroundFrom(CUSTOM_UI_BACKGROUND, 'Custom', customHex);
  }
  // Named rather than UI_BACKGROUNDS[0] — see getUITheme.
  return UI_BACKGROUNDS.find((b) => b.id === id)
    || UI_BACKGROUNDS.find((b) => b.id === DEFAULT_UI_BACKGROUND)
    || UI_BACKGROUNDS[0];
}

/**
 * Write a background into the document, beside the accent.
 *
 * Both the hex and the "r, g, b" triple are published: the app draws these
 * colours solid in some places and as rgba() tints in others, and a tint left
 * on the old hard-coded triple would keep the previous background showing
 * through wherever something is translucent.
 */
export function applyUIBackground(id: string, customHex?: string): void {
  const b = getUIBackground(id, customHex);
  const s = document.documentElement.style;
  s.setProperty('--bg-base', b.base);
  s.setProperty('--bg-surface', b.surface);
  s.setProperty('--bg-raised', b.raised);
  s.setProperty('--bg-sunken', b.sunken);
  s.setProperty('--bg-base-rgb', b.baseRgb);
  s.setProperty('--bg-surface-rgb', b.surfaceRgb);
  s.setProperty('--bg-raised-rgb', b.raisedRgb);
  s.setProperty('--bg-sunken-rgb', b.sunkenRgb);
}

/**
 * Contrast between a colour and the app background. A very dark accent all
 * but disappears against it — black measures 1.09:1 — which is worth telling
 * the user about rather than silently accepting.
 */
export function accentContrastOnBackground(hex: string, backgroundHex?: string): number {
  const rgb = hexToRgb(hex);
  if (!rgb) return 0;
  // Against the background actually in use. This read [13, 13, 26] — the old
  // hard-coded #0d0d1a — which stopped being true the moment the background
  // became a setting: the warning would then be measured against a colour the
  // user is not looking at.
  const bg = (backgroundHex && hexToRgb(backgroundHex)) || [13, 13, 26];
  const a = luminance(rgb);
  const b = luminance(bg);
  const hi = Math.max(a, b);
  const lo = Math.min(a, b);
  return (hi + 0.05) / (lo + 0.05);
}

/** Below this an accent is too close to the background to make out. */
export const MIN_ACCENT_CONTRAST = 2.5;

/** Relative luminance, per WCAG. */
function luminance(rgb: [number, number, number]): number {
  const [r, g, b] = rgb.map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/**
 * Build a full theme from a single colour.
 *
 * Picking seven values by hand is not something to ask of anyone, so the
 * lighter and darker steps are derived, and the text colour for solid fills
 * is chosen by measuring contrast rather than guessing: white fails on bright
 * accents (it is only 2.15:1 on amber), so those get a dark ink instead.
 */
export function buildCustomTheme(hex: string): UITheme | null {
  const rgb = hexToRgb(hex);
  if (!rgb) return null;
  const base = rgbToHex(rgb[0], rgb[1], rgb[2]);
  const lum = luminance(rgb);
  // 0.45 is where white text drops below ~3:1 in practice; above it, use a
  // heavily darkened version of the accent itself so the ink stays in family.
  const ink = lum > 0.45 ? shade(rgb, -0.78) : '#ffffff';
  return {
    id: CUSTOM_UI_THEME,
    name: 'Custom',
    rgb: `${rgb[0]}, ${rgb[1]}, ${rgb[2]}`,
    accent: base,
    light: shade(rgb, 0.25),
    lighter: shade(rgb, 0.5),
    pale: shade(rgb, 0.75),
    dark: shade(rgb, -0.2),
    onAccent: ink,
  };
}
