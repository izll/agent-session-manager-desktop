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
 * Whether Enter already has a native meaning on the focused control.
 *
 * Dialog-level keyboard handlers are useful when focus is on the overlay or a
 * text field, but must not replace the action of a focused button. In
 * particular, the safe/cancel button is intentionally focused first in a
 * confirmation: bubbling its Enter key into a generic "confirm" handler turns
 * the safest keyboard action into the destructive one.
 */
export function dialogEnterBelongsToControl(event: KeyboardEvent): boolean {
  const target = event.target;
  if (!(target instanceof Element)) return false;
  return !!target.closest('button, a[href], [role="button"]');
}

/**
 * Focus the first text field in the dialog, falling back to autoFocusDialog.
 *
 * autoFocusDialog takes the first focusable child, and buttons count — so a
 * dialog whose header carries a dictate or close button focuses that instead of
 * the field the user came to fill in. Where a dialog exists to be typed into,
 * this is what it wants.
 */
export function autoFocusField(node: HTMLElement) {
  return installDialogFocus(node, () => {
    const field = node.querySelector<HTMLInputElement | HTMLTextAreaElement>(TEXT_FIELD_SELECTOR);
    if (!field) return firstFocusable(node);
    // Cursor at the end rather than a selection: an edit dialog opens with the
    // existing text in it, and selecting all of it means one keystroke wipes
    // what the user meant to amend.
    const len = field.value?.length ?? 0;
    try { field.setSelectionRange(len, len); } catch { /* not all inputs allow it */ }
    return field;
  });
}

/**
 * Focus the first "good" focusable child of the element, or the element
 * itself as a fallback. Ensures keyboard events (Escape/Enter/arrow keys)
 * reach the dialog instead of the terminal underneath.
 */
export function autoFocusDialog(node: HTMLElement) {
  return installDialogFocus(node, () => firstFocusable(node));
}

function focusableChildren(node: HTMLElement): HTMLElement[] {
  return [...node.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)]
    // A settings dialog contains controls for inactive tabs in some component
    // trees. They must not become keyboard destinations merely because they
    // match the selector.
    .filter((element) => element.getClientRects().length > 0);
}

function firstFocusable(node: HTMLElement): HTMLElement | null {
  return focusableChildren(node)[0] ?? null;
}

/** Shared focus lifecycle for both dialog actions. */
function installDialogFocus(node: HTMLElement, preferred: () => HTMLElement | null) {
  const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  // Defer one frame so Svelte has finished creating child nodes. Retain the id:
  // a dialog can be opened and synchronously replaced, and a late focus from
  // the removed dialog must not steal focus from its replacement.
  const frame = requestAnimationFrame(() => {
    // A click, a fill operation, or a fast keyboard interaction can focus a
    // dialog control before this deferred callback. That is more specific
    // than our default; moving focus or the selection now can corrupt the
    // first input (for example by appending to a title being replaced).
    if (node.contains(document.activeElement)) return;
    const target = preferred();
    if (target) {
      target.focus();
      if (target instanceof HTMLInputElement && typeof target.value === 'string') {
        const len = target.value.length;
        try { target.setSelectionRange(len, len); } catch { /* unsupported input type */ }
      }
      return;
    }
    // Make the overlay itself focusable as a last resort so Escape works.
    if (!node.hasAttribute('tabindex')) node.setAttribute('tabindex', '-1');
    node.focus();
  });

  function trapTab(event: KeyboardEvent) {
    if (event.key !== 'Tab' || event.defaultPrevented) return;
    const focusable = focusableChildren(node);
    if (focusable.length === 0) {
      event.preventDefault();
      node.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (event.shiftKey && (active === first || !node.contains(active))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (active === last || !node.contains(active))) {
      event.preventDefault();
      first.focus();
    }
  }

  node.addEventListener('keydown', trapTab);
  return {
    destroy() {
      cancelAnimationFrame(frame);
      node.removeEventListener('keydown', trapTab);
      // Nested confirmations are portalled outside their owning dialog. When
      // one closes, return to the control that opened it; otherwise focus falls
      // to <body> and the still-visible parent stops receiving Escape/Tab.
      if (previousFocus?.isConnected &&
          (node.contains(document.activeElement) || document.activeElement === document.body)) {
        previousFocus.focus();
      }
    },
  };
}
