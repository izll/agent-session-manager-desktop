/**
 * Keeps a popup menu inside the window.
 *
 * A menu placed at the cursor runs off the bottom edge when it is opened on a
 * row near the end of a long list, and off the right edge in a narrow window.
 * The fix has to measure rather than estimate: these menus are `position:
 * fixed` with a min-width but no fixed height, so how far they overflow depends
 * on how many entries the menu happens to have.
 */

/** Distance kept between the menu and the window edge. */
const MARGIN = 8;

export interface MenuAnchor {
  x: number;
  y: number;
}

/**
 * Svelte action: positions the node at the anchor, flipped or clamped so it
 * stays on screen.
 *
 * Applied to an element that is already `position: fixed`. It writes `left`
 * and `top` directly, so the caller does not set them itself.
 *
 * Vertically the menu flips ABOVE the cursor when there is no room below —
 * flipping keeps the pointer at the menu's edge, where clamping would drop it
 * into the middle of the entries and put a different item under the cursor
 * than the one at the click. Horizontally it clamps instead: a menu is much
 * wider than the pointer, so flipping it left of the cursor moves it further
 * than needed.
 */
export function menuPosition(node: HTMLElement, anchor: MenuAnchor) {
  function place(a: MenuAnchor) {
    // Measured after the node is in the DOM, so the height reflects the
    // entries this particular menu has.
    const { offsetWidth: w, offsetHeight: h } = node;
    const maxX = window.innerWidth - w - MARGIN;
    const maxY = window.innerHeight - h - MARGIN;

    let top = a.y;
    if (a.y > maxY) {
      // Prefer flipping above the cursor; fall back to clamping when the menu
      // is taller than the space above it too (a very long menu in a short
      // window), where any position overflows and the top edge is the least
      // bad one.
      const above = a.y - h;
      top = above >= MARGIN ? above : Math.max(MARGIN, maxY);
    }

    node.style.left = `${Math.max(MARGIN, Math.min(a.x, maxX))}px`;
    node.style.top = `${top}px`;
  }

  place(anchor);

  return {
    update: place,
  };
}
