/**
 * The pages of a note. Every note — a tab's, a session's, the project's — is
 * a list of titled pages, saved whole: a page switch, rename or reorder is
 * one write of the note, never half of one. Plain data in and out, so the
 * rules can be run under plain node.
 *
 * The backend keeps the same shape (session/note_pages.go). An empty note
 * comes back with no pages; the editor always shows at least one.
 */

export interface NotePage {
  id: string;
  title: string;
  text: string;
}

/** Fixed, like the backend's: the page a text-only note is read as. */
export const FIRST_PAGE_ID = 'page-1';

/** A fresh ID, unique among the pages given. */
export function newPageId(pages: NotePage[]): string {
  const taken = new Set(pages.map((p) => p.id));
  for (;;) {
    const id = `page-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
    if (!taken.has(id)) return id;
  }
}

/**
 * The pages to show for a note as read: never none, so there is always a page
 * to type into, and never anything malformed the backend might have let by.
 */
export function editablePages(pages: readonly Partial<NotePage>[] | null | undefined): NotePage[] {
  const out: NotePage[] = [];
  const seen = new Set<string>();
  for (const page of pages ?? []) {
    let id = typeof page?.id === 'string' && page.id ? page.id : '';
    if (!id || seen.has(id)) id = newPageId(out);
    seen.add(id);
    out.push({
      id,
      title: typeof page?.title === 'string' ? page.title : '',
      text: typeof page?.text === 'string' ? page.text : '',
    });
  }
  if (!out.length) out.push({ id: FIRST_PAGE_ID, title: '', text: '' });
  return out;
}

/**
 * A comparable fingerprint of a note, for "is this saved?". Titles are
 * compared as the backend will store them, so trailing spaces in a title
 * being typed do not count as an unsaved change of their own.
 */
export function pagesKey(pages: readonly NotePage[]): string {
  // One untitled empty page is what an empty note is shown as; it is the
  // same note as no pages at all.
  if (!pages.length || (pages.length === 1 && !pages[0].title.trim() && pages[0].text === '')) return '[]';
  return JSON.stringify(pages.map((p) => [p.id, cleanTitle(p.title), p.text]));
}

/** A title as it is stored: one line, single spaces. */
export function cleanTitle(title: string): string {
  return title.split(/\s+/).filter(Boolean).join(' ');
}

/**
 * The single text a note is shown as where pages are not: the dot and its
 * tooltip on the Notes view tab. The same rule as the backend's compat text
 * (NotePagesText), so it agrees with what the session list carries.
 */
export function notePagesText(pages: readonly NotePage[]): string {
  if (pages.length === 1 && !pages[0].title) return pages[0].text;
  return pages
    .filter((p) => p.text.trim() !== '')
    .map((p) => (p.title ? `# ${p.title}\n${p.text}` : p.text))
    .join('\n\n');
}

/** Whether any page has text in it — whitespace alone is not a note. */
export function hasNoteText(pages: readonly NotePage[] | undefined): boolean {
  return !!pages?.some((p) => p.text.trim() !== '');
}

export function pageIndex(pages: readonly NotePage[], id: string): number {
  return pages.findIndex((p) => p.id === id);
}

/** The active page, or the first when the one asked for is gone. */
export function resolveActivePage(pages: readonly NotePage[], id: string | null | undefined): string {
  return pages.some((p) => p.id === id) ? (id as string) : pages[0]?.id ?? FIRST_PAGE_ID;
}

export function setPageText(pages: readonly NotePage[], id: string, text: string): NotePage[] {
  return pages.map((p) => (p.id === id && p.text !== text ? { ...p, text } : p));
}

export function renamePage(pages: readonly NotePage[], id: string, title: string): NotePage[] {
  const clean = cleanTitle(title);
  return pages.map((p) => (p.id === id ? { ...p, title: clean } : p));
}

/** A new empty page after the one given (or at the end), and its ID. */
export function addPage(pages: readonly NotePage[], afterId: string | null, title = ''): { pages: NotePage[]; id: string } {
  const id = newPageId([...pages]);
  const page: NotePage = { id, title: cleanTitle(title), text: '' };
  const at = afterId === null ? -1 : pageIndex(pages, afterId);
  const next = [...pages];
  next.splice(at === -1 ? next.length : at + 1, 0, page);
  return { pages: next, id };
}

/**
 * Removes a page. The last page is never removed — a note always has one to
 * type into. The page shown stays the same unless it is the one removed; then
 * it is the neighbour that took its place: the one after it, or the one
 * before when it was last.
 */
export function deletePage(pages: readonly NotePage[], id: string, activeId: string): { pages: NotePage[]; activeId: string } {
  const at = pageIndex(pages, id);
  if (at === -1 || pages.length <= 1) return { pages: [...pages], activeId: resolveActivePage(pages, activeId) };
  const next = pages.filter((p) => p.id !== id);
  if (id !== activeId) return { pages: next, activeId: resolveActivePage(next, activeId) };
  return { pages: next, activeId: next[Math.min(at, next.length - 1)].id };
}

/** Moves a page to position `to` (an index into the list without it). */
export function movePage(pages: readonly NotePage[], id: string, to: number): NotePage[] {
  const at = pageIndex(pages, id);
  if (at === -1) return [...pages];
  const next = pages.filter((p) => p.id !== id);
  const clamped = Math.max(0, Math.min(to, next.length));
  next.splice(clamped, 0, pages[at]);
  return next;
}

/**
 * Where a page dropped on another lands, as movePage's `to`: before or after
 * the target, allowing for the dragged page leaving its old place first.
 */
export function dropIndex(pages: readonly NotePage[], draggedId: string, targetId: string, after: boolean): number {
  const from = pageIndex(pages, draggedId);
  let to = pageIndex(pages, targetId) + (after ? 1 : 0);
  if (from !== -1 && from < to) to -= 1;
  return to;
}

/** The page after or before the active one, wrapping round. */
export function stepPage(pages: readonly NotePage[], id: string, delta: number): string {
  if (!pages.length) return id;
  const at = Math.max(0, pageIndex(pages, id));
  return pages[(at + delta + pages.length) % pages.length].id;
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

/**
 * What a page is called on screen: its title, or for an untitled page "Note"
 * when it is the only one and "Note 2" when there are several — a strip of
 * identical labels would not say which is which.
 */
export function pageLabel(title: string, index: number, total: number, t: Translate): string {
  if (title) return title;
  return total <= 1 ? t('notes.untitledPage') : t('notes.untitledPageN', { n: index + 1 });
}
