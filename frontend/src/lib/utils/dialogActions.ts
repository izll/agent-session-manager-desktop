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

/** Fields worth typing into, in the order a dialog usually wants them. */
const TEXT_FIELD_SELECTOR = [
  'input[type="text"]:not([disabled])',
  'input:not([type]):not([disabled])',
  'textarea:not([disabled])',
].join(',');

/**
 * Focus the first text field in the dialog, falling back to autoFocusDialog.
 *
 * autoFocusDialog takes the first focusable child, and buttons count — so a
 * dialog whose header carries a dictate or close button focuses that instead of
 * the field the user came to fill in. Where a dialog exists to be typed into,
 * this is what it wants.
 */
export function autoFocusField(node: HTMLElement) {
  requestAnimationFrame(() => {
    const field = node.querySelector<HTMLInputElement | HTMLTextAreaElement>(TEXT_FIELD_SELECTOR);
    if (!field) {
      autoFocusDialog(node);
      return;
    }
    field.focus();
    // Cursor at the end rather than a selection: an edit dialog opens with the
    // existing text in it, and selecting all of it means one keystroke wipes
    // what the user meant to amend.
    const len = field.value?.length ?? 0;
    try { field.setSelectionRange(len, len); } catch { /* not all inputs allow it */ }
  });
}

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
