// Fix frameless edge-resize under fractional display scaling.
//
// The stock Wails v2 runtime detects the right/bottom window edges with
//   window.outerWidth  - e.clientX < borderThickness
//   window.outerHeight - e.clientY < borderThickness
// On WebKitGTK with fractional scaling (e.g. KDE 125%, Xft.dpi=120)
// outerWidth/outerHeight report DEVICE pixels while clientX/clientY are CSS
// pixels, so the condition can never be true — the window can only be
// resized from the left/top edges (whose check, e.clientX < t, is
// scale-independent). Symptom: resize works only in the top-left corner.
//
// This listener runs AFTER the runtime's own mousemove handler (registered
// later → called later) and recomputes the edge using innerWidth/innerHeight,
// which are CSS pixels like clientX/clientY. It overrides the runtime's
// (possibly cleared) `resizeEdge` flag; the runtime's own mousedown handler
// then picks it up and issues the actual resize.
export function installFramelessResizeFix(): void {
  const w = window as any;

  window.addEventListener('mousemove', (e: MouseEvent) => {
    const f = w.wails?.flags;
    if (!f || !f.enableResize) return;

    const t = f.borderThickness ?? 5;
    const left = e.clientX < t;
    const top = e.clientY < t;
    const right = window.innerWidth - e.clientX < t;
    const bottom = window.innerHeight - e.clientY < t;

    let edge: string | undefined;
    if (right && bottom) edge = 'se-resize';
    else if (left && bottom) edge = 'sw-resize';
    else if (left && top) edge = 'nw-resize';
    else if (top && right) edge = 'ne-resize';
    else if (left) edge = 'w-resize';
    else if (top) edge = 'n-resize';
    else if (bottom) edge = 's-resize';
    else if (right) edge = 'e-resize';

    if (f.resizeEdge !== edge) {
      if (f.defaultCursor == null) {
        f.defaultCursor = document.documentElement.style.cursor;
      }
      document.documentElement.style.cursor = edge || f.defaultCursor;
      f.resizeEdge = edge;
    }
  });
}
