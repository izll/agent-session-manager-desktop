import { Marked } from 'marked';
import DOMPurify, { type DOMPurify as Purifier } from 'dompurify';

// Markdown to HTML for the Files view.
//
// A file in the repository is not trusted: a README can carry raw HTML, and
// an agent writes whatever it was asked to. So everything marked produces goes
// through DOMPurify before it reaches the page — no scripts, no event handlers,
// no styles that could cover the app.

const marked = new Marked({ gfm: true, async: false });

let purifier: Purifier | null = null;

function getPurifier(): Purifier {
  if (purifier) return purifier;
  // An instance of its own, so the hook below does not change what any other
  // use of DOMPurify in the app would let through.
  const instance = DOMPurify(window);
  instance.addHook('afterSanitizeAttributes', (node) => {
    if (node.tagName === 'IMG') {
      // A relative image lives in the repository and is loaded through the
      // backend, which checks it stays inside the browsed directory. Left as
      // it is, the webview would ask the app's own asset server for it.
      const src = node.getAttribute('src') || '';
      if (src && !/^(https?:\/\/|data:image\/)/i.test(src)) {
        node.setAttribute('data-md-src', src);
        node.removeAttribute('src');
      }
    }
    // Links are followed by the view's own click handler; a target would let
    // the webview try to open a window of its own.
    if (node.tagName === 'A') node.removeAttribute('target');
  });
  purifier = instance;
  return instance;
}

export function renderMarkdown(source: string): string {
  const html = marked.parse(source) as string;
  return getPurifier().sanitize(html, {
    FORBID_TAGS: ['style', 'form', 'button', 'textarea', 'select'],
    FORBID_ATTR: ['style'],
  });
}
