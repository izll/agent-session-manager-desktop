// Find in rendered text: wrap every occurrence of a query in a <mark>.
//
// For views whose text is already in the page as HTML — a rendered Markdown
// file — where there is no editor to ask and the match has to be shown where
// the reader is looking. Case is ignored, as in every other find in the app.

const MARK_CLASS = 'find-hit';

/**
 * Marks every match of `query` inside `root` and returns the marks in document
 * order. A match is found within one text node: one split by formatting —
 * "foo **bar**" searched as "foo bar" — is not, which is the price of leaving
 * the markup as it was.
 */
export function markMatches(root: HTMLElement, query: string): HTMLElement[] {
  clearMarks(root);
  const needle = query.toLowerCase();
  if (!needle) return [];

  const nodes: Text[] = [];
  const walker = root.ownerDocument.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    // Only text inside an element of the content: a text node directly under
    // the root may be one the framework placed and will remove by reference.
    if (node.parentNode !== root && node.nodeValue?.toLowerCase().includes(needle)) nodes.push(node as Text);
  }

  const marks: HTMLElement[] = [];
  for (const node of nodes) {
    const found: HTMLElement[] = [];
    let rest = node;
    let at = rest.nodeValue!.toLowerCase().indexOf(needle);
    while (at >= 0) {
      const match = rest.splitText(at);
      rest = match.splitText(needle.length);
      const mark = root.ownerDocument.createElement('mark');
      mark.className = MARK_CLASS;
      match.parentNode!.replaceChild(mark, match);
      mark.appendChild(match);
      found.push(mark);
      at = rest.nodeValue!.toLowerCase().indexOf(needle);
    }
    marks.push(...found);
  }
  return marks;
}

/** Takes the marks out again, leaving the text as it was. */
export function clearMarks(root: HTMLElement): void {
  const parents = new Set<Node>();
  for (const mark of Array.from(root.querySelectorAll(`mark.${MARK_CLASS}`))) {
    const parent = mark.parentNode;
    if (!parent) continue;
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
    parent.removeChild(mark);
    parents.add(parent);
  }
  // Rejoin the pieces the marks split the text into.
  for (const parent of parents) parent.normalize();
}
