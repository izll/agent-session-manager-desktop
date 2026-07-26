import type { ITheme } from '@xterm/xterm';

// Terminal color schemes, in the spirit of Konsole's profile palettes.
// Each entry is a full 16-colour ANSI palette plus background/foreground/
// cursor/selection, so switching one changes the whole look of a pane.
//
// "asmgr" is the app's own palette (the previous hard-coded look) and stays
// the default so existing installs don't change appearance on upgrade.

export interface TerminalTheme {
  id: string;
  /** Shown in Settings; not translated — palette names are proper nouns. */
  name: string;
  theme: ITheme;
}

export const TERMINAL_THEMES: TerminalTheme[] = [
  {
    id: 'asmgr',
    name: 'ASMGR (default)',
    // The app's original look, with one correction: ANSI blue used to be
    // #bd93f9, a Dracula purple that read as violet rather than blue.
    // Deliberately defines no bright* colours —
    // xterm derives them, which is exactly what the pre-palette build did,
    // so upgrading doesn't change how any existing terminal renders.
    theme: {
      background: '#0a0a0f',
      foreground: '#e4e4e7',
      cursor: '#8b5cf6',
      cursorAccent: '#0a0a0f',
      selectionBackground: 'rgba(139, 92, 246, 0.3)',
      black: '#000000',
      red: '#ff5555',
      green: '#50fa7b',
      yellow: '#f1fa8c',
      blue: '#7aa2f7',
      magenta: '#ff79c6',
      cyan: '#8be9fd',
      white: '#f8f8f2',
    },
  },
  {
    id: 'linux',
    name: 'White on Black (classic)',
    // The classic VGA/Linux console palette — the 16 colours a plain xterm
    // starts with, on black.
    theme: {
      background: '#000000',
      foreground: '#b2b2b2',
      cursor: '#b2b2b2',
      cursorAccent: '#000000',
      selectionBackground: 'rgba(178, 178, 178, 0.35)',
      black: '#000000',
      red: '#b21818',
      green: '#18b218',
      yellow: '#b26818',
      blue: '#1818b2',
      magenta: '#b218b2',
      cyan: '#18b2b2',
      white: '#b2b2b2',
      brightBlack: '#686868',
      brightRed: '#ff5454',
      brightGreen: '#54ff54',
      brightYellow: '#ffff54',
      brightBlue: '#5454ff',
      brightMagenta: '#ff54ff',
      brightCyan: '#54ffff',
      brightWhite: '#ffffff',
    },
  },
  {
    id: 'breeze',
    name: 'Breeze (KDE)',
    theme: {
      background: '#31363b',
      foreground: '#eff0f1',
      cursor: '#eff0f1',
      cursorAccent: '#31363b',
      selectionBackground: 'rgba(61, 174, 233, 0.4)',
      black: '#232629',
      red: '#ed1515',
      green: '#11d116',
      yellow: '#f67400',
      blue: '#1d99f3',
      magenta: '#9b59b6',
      cyan: '#1abc9c',
      white: '#fcfcfc',
      brightBlack: '#7f8c8d',
      brightRed: '#c0392b',
      brightGreen: '#1cdc9a',
      brightYellow: '#fdbc4b',
      brightBlue: '#3daee9',
      brightMagenta: '#8e44ad',
      brightCyan: '#16a085',
      brightWhite: '#ffffff',
    },
  },
  {
    id: 'dracula',
    name: 'Dracula',
    theme: {
      background: '#282a36',
      foreground: '#f8f8f2',
      cursor: '#f8f8f2',
      cursorAccent: '#282a36',
      selectionBackground: 'rgba(68, 71, 90, 0.9)',
      black: '#21222c',
      red: '#ff5555',
      green: '#50fa7b',
      yellow: '#f1fa8c',
      blue: '#bd93f9',
      magenta: '#ff79c6',
      cyan: '#8be9fd',
      white: '#f8f8f2',
      brightBlack: '#6272a4',
      brightRed: '#ff6e6e',
      brightGreen: '#69ff94',
      brightYellow: '#ffffa5',
      brightBlue: '#d6acff',
      brightMagenta: '#ff92df',
      brightCyan: '#a4ffff',
      brightWhite: '#ffffff',
    },
  },
  {
    id: 'solarized-dark',
    name: 'Solarized Dark',
    theme: {
      background: '#002b36',
      foreground: '#839496',
      cursor: '#93a1a1',
      cursorAccent: '#002b36',
      selectionBackground: 'rgba(7, 54, 66, 0.99)',
      black: '#073642',
      red: '#dc322f',
      green: '#859900',
      yellow: '#b58900',
      blue: '#268bd2',
      magenta: '#d33682',
      cyan: '#2aa198',
      white: '#eee8d5',
      brightBlack: '#002b36',
      brightRed: '#cb4b16',
      brightGreen: '#586e75',
      brightYellow: '#657b83',
      brightBlue: '#839496',
      brightMagenta: '#6c71c4',
      brightCyan: '#93a1a1',
      brightWhite: '#fdf6e3',
    },
  },
  {
    id: 'solarized-light',
    name: 'Solarized Light',
    theme: {
      background: '#fdf6e3',
      foreground: '#657b83',
      cursor: '#586e75',
      cursorAccent: '#fdf6e3',
      selectionBackground: 'rgba(238, 232, 213, 0.99)',
      black: '#073642',
      red: '#dc322f',
      green: '#859900',
      yellow: '#b58900',
      blue: '#268bd2',
      magenta: '#d33682',
      cyan: '#2aa198',
      white: '#eee8d5',
      brightBlack: '#002b36',
      brightRed: '#cb4b16',
      brightGreen: '#586e75',
      brightYellow: '#657b83',
      brightBlue: '#839496',
      brightMagenta: '#6c71c4',
      brightCyan: '#93a1a1',
      brightWhite: '#fdf6e3',
    },
  },
  {
    id: 'gruvbox-dark',
    name: 'Gruvbox Dark',
    theme: {
      background: '#282828',
      foreground: '#ebdbb2',
      cursor: '#ebdbb2',
      cursorAccent: '#282828',
      selectionBackground: 'rgba(80, 73, 69, 0.9)',
      black: '#282828',
      red: '#cc241d',
      green: '#98971a',
      yellow: '#d79921',
      blue: '#458588',
      magenta: '#b16286',
      cyan: '#689d6a',
      white: '#a89984',
      brightBlack: '#928374',
      brightRed: '#fb4934',
      brightGreen: '#b8bb26',
      brightYellow: '#fabd2f',
      brightBlue: '#83a598',
      brightMagenta: '#d3869b',
      brightCyan: '#8ec07c',
      brightWhite: '#ebdbb2',
    },
  },
  {
    id: 'nord',
    name: 'Nord',
    theme: {
      background: '#2e3440',
      foreground: '#d8dee9',
      cursor: '#d8dee9',
      cursorAccent: '#2e3440',
      selectionBackground: 'rgba(67, 76, 94, 0.9)',
      black: '#3b4252',
      red: '#bf616a',
      green: '#a3be8c',
      yellow: '#ebcb8b',
      blue: '#81a1c1',
      magenta: '#b48ead',
      cyan: '#88c0d0',
      white: '#e5e9f0',
      brightBlack: '#4c566a',
      brightRed: '#bf616a',
      brightGreen: '#a3be8c',
      brightYellow: '#ebcb8b',
      brightBlue: '#81a1c1',
      brightMagenta: '#b48ead',
      brightCyan: '#8fbcbb',
      brightWhite: '#eceff4',
    },
  },
  {
    id: 'tokyo-night',
    name: 'Tokyo Night',
    theme: {
      background: '#1a1b26',
      foreground: '#c0caf5',
      cursor: '#c0caf5',
      cursorAccent: '#1a1b26',
      selectionBackground: 'rgba(40, 52, 87, 0.9)',
      black: '#15161e',
      red: '#f7768e',
      green: '#9ece6a',
      yellow: '#e0af68',
      blue: '#7aa2f7',
      magenta: '#bb9af7',
      cyan: '#7dcfff',
      white: '#a9b1d6',
      brightBlack: '#414868',
      brightRed: '#f7768e',
      brightGreen: '#9ece6a',
      brightYellow: '#e0af68',
      brightBlue: '#7aa2f7',
      brightMagenta: '#bb9af7',
      brightCyan: '#7dcfff',
      brightWhite: '#c0caf5',
    },
  },
  {
    id: 'one-dark',
    name: 'One Dark',
    theme: {
      background: '#282c34',
      foreground: '#abb2bf',
      cursor: '#528bff',
      cursorAccent: '#282c34',
      selectionBackground: 'rgba(62, 68, 81, 0.9)',
      black: '#282c34',
      red: '#e06c75',
      green: '#98c379',
      yellow: '#e5c07b',
      blue: '#61afef',
      magenta: '#c678dd',
      cyan: '#56b6c2',
      white: '#abb2bf',
      brightBlack: '#5c6370',
      brightRed: '#e06c75',
      brightGreen: '#98c379',
      brightYellow: '#e5c07b',
      brightBlue: '#61afef',
      brightMagenta: '#c678dd',
      brightCyan: '#56b6c2',
      brightWhite: '#ffffff',
    },
  },
  {
    id: 'black-on-white',
    name: 'Black on White',
    theme: {
      background: '#ffffff',
      foreground: '#000000',
      cursor: '#000000',
      cursorAccent: '#ffffff',
      selectionBackground: 'rgba(180, 213, 255, 0.99)',
      black: '#000000',
      red: '#b21818',
      green: '#18b218',
      yellow: '#b26818',
      blue: '#1818b2',
      magenta: '#b218b2',
      cyan: '#18b2b2',
      white: '#b2b2b2',
      brightBlack: '#686868',
      brightRed: '#ff5454',
      brightGreen: '#54ff54',
      brightYellow: '#ffff54',
      brightBlue: '#5454ff',
      brightMagenta: '#ff54ff',
      brightCyan: '#54ffff',
      brightWhite: '#ffffff',
    },
  },
];

export const DEFAULT_TERMINAL_THEME = 'asmgr';

export function getTerminalTheme(id: string | undefined): ITheme {
  const found = TERMINAL_THEMES.find(t => t.id === id);
  return (found || TERMINAL_THEMES[0]).theme;
}

/** Editable starting point for a user-defined palette. */
export const CUSTOM_THEME_PREFIX = 'custom:';

/** A user-defined palette as stored in settings. */
export interface CustomPalette {
  id: string;
  name: string;
  colors: Record<string, string>;
}

/** All pickable palettes: built-ins first, then the user's own. */
export function allPalettes(custom: CustomPalette[] | null | undefined): TerminalTheme[] {
  const user = (custom || []).map(c => ({
    id: c.id,
    name: c.name || 'Custom',
    theme: { ...TERMINAL_THEMES[0].theme, ...(c.colors || {}) } as ITheme,
  }));
  return [...TERMINAL_THEMES, ...user];
}

/** Next free id for a new user palette. */
export function nextCustomId(custom: CustomPalette[] | null | undefined): string {
  let n = 1;
  const taken = new Set((custom || []).map(c => c.id));
  while (taken.has(`${CUSTOM_THEME_PREFIX}${n}`)) n++;
  return `${CUSTOM_THEME_PREFIX}${n}`;
}

/** The colour keys a custom palette can define, in UI order. */
export const CUSTOM_THEME_KEYS: Array<{ key: string; label: string }> = [
  { key: 'background', label: 'Background' },
  { key: 'foreground', label: 'Foreground' },
  { key: 'cursor', label: 'Cursor' },
  { key: 'selectionBackground', label: 'Selection' },
  { key: 'black', label: 'Black' },
  { key: 'red', label: 'Red' },
  { key: 'green', label: 'Green' },
  { key: 'yellow', label: 'Yellow' },
  { key: 'blue', label: 'Blue' },
  { key: 'magenta', label: 'Magenta' },
  { key: 'cyan', label: 'Cyan' },
  { key: 'white', label: 'White' },
  { key: 'brightBlack', label: 'Bright black' },
  { key: 'brightRed', label: 'Bright red' },
  { key: 'brightGreen', label: 'Bright green' },
  { key: 'brightYellow', label: 'Bright yellow' },
  { key: 'brightBlue', label: 'Bright blue' },
  { key: 'brightMagenta', label: 'Bright magenta' },
  { key: 'brightCyan', label: 'Bright cyan' },
  { key: 'brightWhite', label: 'Bright white' },
];

/**
 * Resolve the palette for one pane. Terminals and agents are two separate
 * worlds — a terminal never inherits an agent palette and vice versa:
 *
 *   plain terminal : tab override → terminalDefault
 *   agent pane     : tab override → per-agent-type → agentDefault
 */
export function resolveTerminalTheme(opts: {
  tabTheme?: string;
  agent?: string;
  agentThemes?: Record<string, string> | null;
  terminalDefault?: string;
  agentDefault?: string;
  customThemes?: CustomPalette[] | null;
}): ITheme {
  const isTerminal = !opts.agent || opts.agent === 'terminal';

  const id = isTerminal
    ? (opts.tabTheme || opts.terminalDefault || DEFAULT_TERMINAL_THEME)
    : (opts.tabTheme ||
       (opts.agent ? opts.agentThemes?.[opts.agent] : '') ||
       opts.agentDefault ||
       DEFAULT_TERMINAL_THEME);

  // A user palette id looks like "custom:<n>"; fall back to the default
  // palette for any colour the user hasn't defined.
  if (id.startsWith(CUSTOM_THEME_PREFIX)) {
    const found = (opts.customThemes || []).find(c => c.id === id);
    if (found) {
      return { ...TERMINAL_THEMES[0].theme, ...(found.colors || {}) } as ITheme;
    }
    return TERMINAL_THEMES[0].theme;
  }
  return getTerminalTheme(id);
}

// --- Font size -----------------------------------------------------------

/** Matches the bounds enforced in app.go; a size outside them is unusable. */
export const MIN_FONT_SIZE = 8;
export const MAX_FONT_SIZE = 32;
// 13, not 14: that is what terminals actually rendered at before the size
// became configurable (Terminal.svelte passed 13 and it won over the 14 in
// createTerminal), so this keeps existing installs looking unchanged.
export const DEFAULT_FONT_SIZE = 13;

/**
 * The size a terminal should use: the tab's own override if it has one,
 * otherwise the global setting, otherwise the built-in default. Zero means
 * "not set" at every level, so an untouched config keeps the original size.
 */
export function resolveFontSize(
  tabSize?: number,
  terminalDefault?: number,
  agentDefault?: number,
  agent?: string,
): number {
  // Only a positive value counts as "set". A negative one (corrupt config, a
  // hand-edited file) is truthy in JS and would otherwise win over the global
  // setting and then clamp to the minimum.
  const usable = (n?: number) => (typeof n === 'number' && n > 0 ? n : 0);
  // Terminal tabs and agent tabs have separate defaults and never fall back
  // to each other's, matching how the colour palettes work.
  const isTerminal = !agent || agent === 'terminal';
  const scoped = isTerminal ? usable(terminalDefault) : usable(agentDefault);
  const size = usable(tabSize) || scoped || DEFAULT_FONT_SIZE;
  return Math.min(MAX_FONT_SIZE, Math.max(MIN_FONT_SIZE, size));
}

/**
 * Whether the view bar should be hidden for a tab. The per-tab value is
 * tri-state (0 inherit, 1 hide, 2 show) so "show this one" survives a
 * default of hidden; terminal and agent tabs have separate defaults.
 */
export function resolveViewBarHidden(
  tabState?: number,
  terminalDefault?: boolean,
  agentDefault?: boolean,
  agent?: string,
): boolean {
  if (tabState === 1) return true;
  if (tabState === 2) return false;
  const isTerminal = !agent || agent === 'terminal';
  return !!(isTerminal ? terminalDefault : agentDefault);
}
