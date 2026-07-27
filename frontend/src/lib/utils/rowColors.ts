/**
 * Shared colour vocabulary for anything that can be tinted in the sidebar
 * (sessions and groups). Kept in one module so the picker dialog and the rows
 * that render the result can never drift apart — a gradient name offered by the
 * dialog but unknown to the renderer would silently paint nothing.
 */

// Gradients from TUI
export const gradients: Record<string, string[]> = {
  'gradient-rainbow':  ['#FF0000', '#FF7F00', '#FFFF00', '#00FF00', '#00FFFF', '#0000FF', '#8B00FF'],
  'gradient-sunset':   ['#FF512F', '#F09819', '#FF8C00', '#DD2476', '#FF416C'],
  'gradient-ocean':    ['#00D2FF', '#3A7BD5', '#00D2D3', '#54A0FF', '#2E86DE'],
  'gradient-forest':   ['#134E5E', '#11998E', '#38EF7D', '#A8E063', '#56AB2F'],
  'gradient-fire':     ['#FF0000', '#FF4500', '#FF6347', '#FF8C00', '#FFD700'],
  'gradient-ice':      ['#E0FFFF', '#B0E0E6', '#87CEEB', '#00CED1', '#4682B4'],
  'gradient-neon':     ['#FF00FF', '#00FFFF', '#39FF14', '#FF6600', '#BF00FF'],
  'gradient-galaxy':   ['#0F0C29', '#302B63', '#8E2DE2', '#4A00E0', '#24243E'],
  'gradient-pastel':   ['#FFB6C1', '#FFDAB9', '#FFFACD', '#98FB98', '#ADD8E6', '#E6E6FA'],
  'gradient-pink':     ['#FF69B4', '#FF1493', '#DB7093', '#FF69B4'],
  'gradient-blue':     ['#00BFFF', '#1E90FF', '#4169E1', '#0000FF', '#4169E1', '#1E90FF'],
  'gradient-green':    ['#00FF00', '#32CD32', '#228B22', '#006400', '#228B22', '#32CD32'],
  'gradient-gold':     ['#FFD700', '#FFA500', '#FF8C00', '#FFA500', '#FFD700'],
  'gradient-purple':   ['#9400D3', '#8A2BE2', '#9932CC', '#BA55D3', '#9932CC', '#8A2BE2'],
  'gradient-cyber':    ['#00FF00', '#00FFFF', '#FF00FF', '#00FFFF', '#00FF00'],
};

// Color options from TUI
export const colorOptions = [
  { name: 'none', color: '' },
  { name: 'auto', color: 'auto' },
  { name: 'black', color: '#000000' },
  { name: 'white', color: '#FFFFFF' },
  { name: 'red', color: '#FF6B6B' },
  { name: 'orange', color: '#FFA500' },
  { name: 'yellow', color: '#FFD93D' },
  { name: 'lime', color: '#ADFF2F' },
  { name: 'green', color: '#6BCB77' },
  { name: 'teal', color: '#20B2AA' },
  { name: 'cyan', color: '#4DD0E1' },
  { name: 'sky', color: '#87CEEB' },
  { name: 'blue', color: '#6C9EFF' },
  { name: 'indigo', color: '#7B68EE' },
  { name: 'purple', color: '#B388FF' },
  { name: 'magenta', color: '#FF00FF' },
  { name: 'pink', color: '#FF8FAB' },
  { name: 'rose', color: '#FF69B4' },
  { name: 'coral', color: '#FF7F50' },
  { name: 'gold', color: '#FFD700' },
  { name: 'silver', color: '#C0C0C0' },
  { name: 'gray', color: '#888888' },
  { name: 'dark-red', color: '#8B0000' },
  { name: 'dark-green', color: '#006400' },
  { name: 'dark-blue', color: '#00008B' },
  { name: 'dark-purple', color: '#4B0082' },
];

// Gradient options
export const gradientOptions = Object.keys(gradients).map(name => ({ name, color: name }));

export function isGradient(color: string): boolean {
  return !!color && color.startsWith('gradient-');
}

/** Turns a gradient name into a CSS value; plain colours pass through unchanged. */
export function getGradientCSS(colorValue: string): string {
  if (isGradient(colorValue)) {
    const colors = gradients[colorValue];
    if (colors) {
      return `linear-gradient(90deg, ${colors.join(', ')})`;
    }
  }
  return colorValue;
}

/** Black or white, whichever stays readable on the given background. */
export function getContrastColor(bgColor: string): string {
  if (!bgColor || bgColor === 'auto') return '#FFFFFF';
  const hex = bgColor.replace('#', '');
  if (hex.length !== 6) return '#FFFFFF';
  const r = parseInt(hex.slice(0, 2), 16);
  const g = parseInt(hex.slice(2, 4), 16);
  const b = parseInt(hex.slice(4, 6), 16);
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
  return luminance > 0.5 ? '#000000' : '#FFFFFF';
}

/**
 * Background style for a whole row, used when the user asked for "full row".
 * The 20 alpha suffix keeps the row readable against the dark sidebar — a solid
 * fill would drown the status lines and badges drawn on top of it.
 */
export function getRowBackgroundStyle(bgColor: string, fullRow: boolean): string {
  if (!fullRow || !bgColor || bgColor === 'auto' || isGradient(bgColor)) return '';
  return `background: ${bgColor}20;`;
}

/**
 * Style for the name label itself: the background chip (unless it was already
 * painted across the whole row) plus the text colour, with 'auto'/unset text
 * falling back to whatever contrasts with the chosen background.
 */
export function getNameStyle(color: string, bgColor: string, fullRow: boolean): string {
  let style = '';
  const hasFlatBg = !!bgColor && bgColor !== 'auto' && !isGradient(bgColor);

  // Padding/rounding only when a chip is actually drawn, so an uncoloured name
  // keeps its original metrics and rows don't jump height when colours change.
  if (hasFlatBg && !fullRow) {
    style += `background-color: ${bgColor}; padding: 1px 6px; border-radius: 4px;`;
  }

  if (color && color !== 'auto' && !isGradient(color)) {
    style += `color: ${color};`;
  } else if ((!color || color === 'auto') && hasFlatBg) {
    style += `color: ${getContrastColor(bgColor)};`;
  }

  return style;
}
