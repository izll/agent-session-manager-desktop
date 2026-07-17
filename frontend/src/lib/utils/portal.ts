// Svelte action: move the element to document.body.
//
// Needed for position:fixed popovers (context menus, dropdowns) that are
// DECLARED inside the sidebar's session list: .session-list is pinned to its
// own compositor layer (transform + contain:paint — the black-corner fix),
// which per spec turns it into the containing block for fixed-position
// descendants. Left in place, a menu positioned from clientX/clientY would
// be offset by the list's position and scroll. At body level the viewport
// is the containing block again and the coordinates are correct.
export function portal(node: HTMLElement) {
  document.body.appendChild(node);
  return {
    destroy() {
      if (node.parentNode) {
        node.parentNode.removeChild(node);
      }
    }
  };
}
