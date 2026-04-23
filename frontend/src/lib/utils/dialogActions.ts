// Svelte use:action helpers for dialogs.
//
// Every dialog in the app renders a `.dialog-overlay` wrapper when `show`
// is true. Previously the wrapper was inert: opening a dialog via keyboard
// left focus on whatever had it before (typically the terminal), so the
// dialog's own Escape/Enter handlers never fired and keystrokes ended up
// going to the agent. `autoFocusDialog` fixes that by grabbing focus as
// soon as the element mounts.

const FOCUSABLE_SELECTOR = [
  'input:not([type="hidden"]):not([disabled])',
  'textarea:not([disabled])',
  'select:not([disabled])',
  'button:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

/**
 * Focus the first "good" focusable child of the element, or the element
 * itself as a fallback. Ensures keyboard events (Escape/Enter/arrow keys)
 * reach the dialog instead of the terminal underneath.
 */
export function autoFocusDialog(node: HTMLElement) {
  // Defer one frame so Svelte has finished creating child nodes.
  requestAnimationFrame(() => {
    const first = node.querySelector<HTMLElement>(FOCUSABLE_SELECTOR);
    if (first) {
      first.focus();
      // If it's a text input, put the cursor at the end rather than select.
      if (first instanceof HTMLInputElement && typeof first.value === 'string') {
        const len = first.value.length;
        try { first.setSelectionRange(len, len); } catch { /* some input types don't support it */ }
      }
    } else {
      // Make the overlay itself focusable as a last resort so Escape works.
      if (!node.hasAttribute('tabindex')) {
        node.setAttribute('tabindex', '-1');
      }
      node.focus();
    }
  });
}
