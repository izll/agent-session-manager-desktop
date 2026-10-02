/**
 * The notes editor's own colours: a background, and a text colour that is
 * either chosen or worked out from that background.
 *
 * The notes are long-form reading and writing, where the near-black of the
 * rest of the app is tiring; so they get a background of their own, without
 * touching the interface around them. An empty setting is the look the notes
 * always had, so nothing changes for anyone who never opens the setting.
 *
 * Plain data in and out, with no imports, so the rules run under plain node.
 */

export interface NotesColor {
  id: string;
  hex: string;
  /** i18n key of the swatch's name. */
  labelKey: string;
}

/** The background the notes always had: no setting stored. */
export const DEFAULT_NOTES_BACKGROUND = '';
/** The text colour derived from the background: no setting stored. */
export const AUTO_NOTES_TEXT = '';
/** The id of the user's own colour, for either setting. */
export const CUSTOM_NOTES_COLOR = 'custom';

/**
 * The stock backgrounds, besides the default. A step lighter than the app, a
 * warm paper for anyone who wants the notes to read like a page, and a plain
 * light one.
 */
export const NOTES_BACKGROUNDS: NotesColor[] = [
  { id: 'dim', hex: '#2a2c31', labelKey: 'settings.notesBgDim' },
  { id: 'paper', hex: '#f4ecd8', labelKey: 'settings.notesBgPaper' },
  { id: 'light', hex: '#f7f7f8', labelKey: 'settings.notesBgLight' },
];

/** The stock text colours, besides Auto: two for dark backgrounds, two for light. */
export const NOTES_TEXT_COLORS: NotesColor[] = [
  { id: 'white', hex: '#f4f4f5', labelKey: 'settings.notesTextWhite' },
  { id: 'grey', hex: '#b4b8c0', labelKey: 'settings.notesTextGrey' },
  { id: 'ink', hex: '#1f2937', labelKey: 'settings.notesTextInk' },
  { id: 'sepia', hex: '#5b4636', labelKey: 'settings.notesTextSepia' },
];

/** The text of the default look, kept exactly: Auto on the default background. */
export const DEFAULT_NOTES_TEXT = '#ffffff';

/**
 * Below this a chosen text colour is hard to make out on its background.
 * WCAG's floor for large text; body text wants 4.5, but a warning that fires
 * on every slightly soft grey would be ignored, and then it warns of nothing.
 */
export const MIN_NOTES_CONTRAST = 3;

export type RGB = [number, number, number];

/** "#rgb" or "#rrggbb", the "#" optional, any case; null for anything else. */
export function parseHex(hex: string | null | undefined): RGB | null {
  if (typeof hex !== 'string') return null;
  const m = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return null;
  let v = m[1];
  if (v.length === 3) v = v.split('').map((c) => c + c).join('');
  return [parseInt(v.slice(0, 2), 16), parseInt(v.slice(2, 4), 16), parseInt(v.slice(4, 6), 16)];
}

function toHex(rgb: RGB): string {
  return '#' + rgb.map((n) => Math.round(Math.min(255, Math.max(0, n))).toString(16).padStart(2, '0')).join('');
}

/** A colour as "#rrggbb", or null when it is not one. */
export function normaliseHex(hex: string | null | undefined): string | null {
  const rgb = parseHex(hex);
  return rgb ? toHex(rgb) : null;
}

/** Relative luminance, per WCAG. */
function luminance([r, g, b]: RGB): number {
  const lin = (v: number) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** WCAG contrast ratio of two colours, 1 to 21; 1 when either is not a colour. */
export function contrastRatio(a: string, b: string): number {
  const ra = parseHex(a);
  const rb = parseHex(b);
  if (!ra || !rb) return 1;
  const la = luminance(ra);
  const lb = luminance(rb);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** Mix a colour towards another; t = 0 is the first, 1 the second. */
function mix(a: RGB, b: RGB, t: number): RGB {
  return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t];
}

/**
 * The text colour that reads best on a background: dark on a light one,
 * light on a dark one.
 *
 * Measured rather than split at a fixed luminance, because the crossover is
 * not where it looks — on a mid grey the dark text wins. The candidates are
 * the background itself taken most of the way to black or to white, so the
 * ink stays in its family (a brown ink on paper, not a blue-black); if the
 * better of them is still short of 4.5:1 — a background in the middle —
 * plain black or white is used instead.
 */
export function readableTextOn(backgroundHex: string): string {
  const bg = parseHex(backgroundHex);
  if (!bg) return DEFAULT_NOTES_TEXT;
  const bgHex = toHex(bg);
  const pick = (dark: string, light: string) =>
    contrastRatio(dark, bgHex) >= contrastRatio(light, bgHex) ? dark : light;
  const tinted = pick(toHex(mix(bg, [0, 0, 0], 0.85)), toHex(mix(bg, [255, 255, 255], 0.9)));
  return contrastRatio(tinted, bgHex) >= 4.5 ? tinted : pick('#000000', '#ffffff');
}

/** Whether dark text is what reads on this background. */
export function isLightBackground(backgroundHex: string): boolean {
  const text = parseHex(readableTextOn(backgroundHex));
  return !!text && luminance(text) < 0.2;
}

/**
 * The background chosen, as a colour; null for the default look. An unknown
 * id or an unusable custom colour falls back to the default rather than to
 * nothing.
 */
export function resolveNotesBackground(id: string | null | undefined, customHex?: string | null): string | null {
  if (!id) return null;
  if (id === CUSTOM_NOTES_COLOR) return normaliseHex(customHex);
  return NOTES_BACKGROUNDS.find((b) => b.id === id)?.hex ?? null;
}

/** The text colour chosen; null for Auto. Unknown or unusable is Auto. */
export function resolveNotesText(id: string | null | undefined, customHex?: string | null): string | null {
  if (!id) return null;
  if (id === CUSTOM_NOTES_COLOR) return normaliseHex(customHex);
  return NOTES_TEXT_COLORS.find((c) => c.id === id)?.hex ?? null;
}

/**
 * What the default background actually looks like: the textarea is a 20%
 * black wash over the content surface, so the colour on screen depends on the
 * interface background under it. Used where a colour has to be measured
 * against it — the Auto swatch and the contrast warning.
 */
export function defaultNotesBackgroundOver(surfaceHex: string): string {
  const surface = parseHex(surfaceHex) ?? [10, 10, 15];
  return toHex(mix(surface, [0, 0, 0], 0.2));
}

/** The text colour in use: the chosen one, or Auto's for the background. */
export function effectiveNotesText(
  bgId: string | null | undefined, bgCustom: string | null | undefined,
  textId: string | null | undefined, textCustom: string | null | undefined,
): string {
  const chosen = resolveNotesText(textId, textCustom);
  if (chosen) return chosen;
  const background = resolveNotesBackground(bgId, bgCustom);
  return background ? readableTextOn(background) : DEFAULT_NOTES_TEXT;
}

function rgba(hex: string, alpha: number): string {
  const [r, g, b] = parseHex(hex) ?? [255, 255, 255];
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

/**
 * The CSS variables the notes view is drawn with, for the settings given.
 * Empty for the default look, so the stylesheet's own values apply and an
 * install that never set anything looks exactly as before.
 *
 * Only one or two colours are chosen; the rest follow from them so the text
 * stays readable whatever the pairing: the placeholder is the text faded, the
 * caret is the text, the scrollbar is drawn in the text colour so it shows on
 * a pale page as well as a dark one, and the border flips with the
 * background's lightness.
 */
export function notesColorVars(
  bgId: string | null | undefined, bgCustom: string | null | undefined,
  textId: string | null | undefined, textCustom: string | null | undefined,
): Record<string, string> {
  const background = resolveNotesBackground(bgId, bgCustom);
  const chosenText = resolveNotesText(textId, textCustom);
  if (!background && !chosenText) return {};
  const text = effectiveNotesText(bgId, bgCustom, textId, textCustom);
  const vars: Record<string, string> = {
    '--notes-fg': text,
    '--notes-placeholder': rgba(text, 0.45),
    '--notes-scrollbar': rgba(text, 0.3),
    '--notes-scrollbar-hover': rgba(text, 0.5),
  };
  if (background) {
    const light = isLightBackground(background);
    vars['--notes-bg'] = background;
    vars['--notes-border'] = light ? 'rgba(0, 0, 0, 0.14)' : 'rgba(255, 255, 255, 0.1)';
    vars['--notes-selection-alpha'] = light ? '0.28' : '0.4';
  }
  return vars;
}

/** The variables as an inline style attribute. */
export function notesColorStyle(vars: Record<string, string>): string {
  return Object.entries(vars).map(([k, v]) => `${k}: ${v}`).join('; ');
}
