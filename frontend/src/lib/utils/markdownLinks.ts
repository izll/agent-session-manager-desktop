// Where the links and images of a rendered Markdown file lead.
//
// Kept apart from the renderer, with no DOM in it, so what a link resolves to
// can be tested on its own: this is the part that decides whether a click
// opens a browser, scrolls, or opens another file of the repository.

/** Extensions shown rendered in the Files view. */
const MARKDOWN_EXTENSIONS = ['md', 'markdown', 'mdown', 'mkd'];

export function isMarkdownPath(path: string): boolean {
  const name = path.split('/').pop() || '';
  const dot = name.lastIndexOf('.');
  if (dot <= 0) return false;
  return MARKDOWN_EXTENSIONS.includes(name.slice(dot + 1).toLowerCase());
}

export type MarkdownLink =
  | { kind: 'external'; url: string }
  | { kind: 'anchor'; slug: string }
  | { kind: 'file'; path: string; slug: string }
  | { kind: 'ignore' };

/**
 * What a link in a file at `fromFile` points to.
 *
 * Only web and mail links leave the app. A link to another file of the
 * repository is followed inside it — but never out of the browsed directory,
 * and never by a scheme the webview would act on by itself.
 */
export function classifyLink(href: string, fromFile: string): MarkdownLink {
  const trimmed = href.trim();
  if (!trimmed) return { kind: 'ignore' };
  if (/^(https?:\/\/|mailto:)/i.test(trimmed)) return { kind: 'external', url: trimmed };
  if (trimmed.startsWith('#')) return { kind: 'anchor', slug: decodePart(trimmed.slice(1)) };
  // Any other scheme (javascript:, file:, data:, vscode:…) or a
  // protocol-relative URL is not something to follow from a document.
  if (/^[a-z][a-z0-9+.-]*:/i.test(trimmed) || trimmed.startsWith('//')) return { kind: 'ignore' };

  const hash = trimmed.indexOf('#');
  const target = hash >= 0 ? trimmed.slice(0, hash) : trimmed;
  const slug = hash >= 0 ? decodePart(trimmed.slice(hash + 1)) : '';
  const path = resolveRelativePath(fromFile, target.split('?')[0]);
  if (path === null) return { kind: 'ignore' };
  return { kind: 'file', path, slug };
}

/**
 * Resolves `target` against the directory of `fromFile`, both relative to the
 * browsed root. A leading slash means the root itself, as it does on GitHub.
 *
 * Returns null for a path that would climb out of the root.
 */
export function resolveRelativePath(fromFile: string, target: string): string | null {
  const decoded = decodePart(target);
  if (!decoded) return null;
  const parts = decoded.startsWith('/')
    ? []
    : fromFile.split('/').slice(0, -1).filter(Boolean);
  for (const segment of decoded.split('/')) {
    if (segment === '' || segment === '.') continue;
    if (segment === '..') {
      if (parts.length === 0) return null;
      parts.pop();
      continue;
    }
    parts.push(segment);
  }
  return parts.length > 0 ? parts.join('/') : null;
}

/**
 * The anchor GitHub gives a heading: lower case, punctuation dropped, spaces
 * turned into hyphens. Accented letters stay, as they do there.
 */
export function headingSlug(text: string): string {
  return text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M}\s_-]/gu, '')
    .replace(/\s/g, '-');
}

/**
 * Anchors for headings in document order. A repeated heading gets -1, -2 …
 * appended, the way GitHub numbers them, so a link to the second one works.
 */
export function headingSlugs(texts: string[]): string[] {
  const seen = new Map<string, number>();
  return texts.map((text) => {
    const base = headingSlug(text);
    const count = seen.get(base) ?? 0;
    seen.set(base, count + 1);
    return count === 0 ? base : `${base}-${count}`;
  });
}

function decodePart(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
