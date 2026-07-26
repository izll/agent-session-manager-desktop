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

export const UI_THEMES: UITheme[] = [
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
];

export const DEFAULT_UI_THEME = 'violet';

export function getUITheme(id: string, customHex?: string): UITheme {
  if (id === CUSTOM_UI_THEME && customHex) {
    const built = buildCustomTheme(customHex);
    if (built) return built;
  }
  return UI_THEMES.find((t) => t.id === id) || UI_THEMES[0];
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
 * Contrast between a colour and the app background. A very dark accent all
 * but disappears against it — black measures 1.09:1 — which is worth telling
 * the user about rather than silently accepting.
 */
export function accentContrastOnBackground(hex: string): number {
  const rgb = hexToRgb(hex);
  if (!rgb) return 0;
  const a = luminance(rgb);
  const b = luminance([13, 13, 26]); // #0d0d1a, the app background
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
