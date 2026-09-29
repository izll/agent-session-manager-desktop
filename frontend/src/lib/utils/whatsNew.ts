/**
 * The "what's new" dialog's logic, kept out of the component so it can be
 * tested on its own: reading the changelog's inline markdown, and arranging
 * the releases into pages.
 */

export interface ChangelogSection {
  kind: string;
  title: string;
  intro: string[] | null;
  items: string[] | null;
}

export interface ChangelogEntry {
  version: string;
  date: string;
  intro: string[] | null;
  sections: ChangelogSection[] | null;
}

// ── Inline markdown ─────────────────────────────────────────────────────────
//
// The notes use a small subset: **bold** lead-ins, `code`, [links](url) and
// the odd backslash escape. They are turned into segments that the component
// renders as elements with text content — never as HTML — so a "<script>" in
// the changelog is shown as those characters and cannot run.

export type Inline =
  | { type: 'text'; text: string }
  | { type: 'code'; text: string }
  | { type: 'strong'; children: Inline[] }
  | { type: 'link'; href: string; children: Inline[] };

/** Only web and mail links open; anything else (javascript:, file:) is text. */
export function safeHref(href: string): string | null {
  const trimmed = href.trim();
  return /^(https?:\/\/|mailto:)/i.test(trimmed) ? trimmed : null;
}

const LINK = /^\[([^\]]+)\]\(([^()\s]+)\)/;

/**
 * Split one line of notes into segments. Bold may hold code and links; a
 * link may hold code. Anything unmatched — a lone "**" or "`" — stays as the
 * characters it is.
 */
export function parseInline(source: string, inside: 'top' | 'strong' | 'link' = 'top'): Inline[] {
  const out: Inline[] = [];
  let text = '';
  const pushText = () => {
    if (text) out.push({ type: 'text', text });
    text = '';
  };

  let i = 0;
  while (i < source.length) {
    const ch = source[i];

    if (ch === '\\' && i + 1 < source.length && /[\\`*_[\]()#+\-.!<>]/.test(source[i + 1])) {
      text += source[i + 1];
      i += 2;
      continue;
    }

    if (ch === '`') {
      const end = source.indexOf('`', i + 1);
      if (end > i + 1) {
        pushText();
        out.push({ type: 'code', text: source.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    }

    if (ch === '*' && source[i + 1] === '*' && inside === 'top') {
      const end = closingStrong(source, i + 2);
      if (end > i + 2) {
        pushText();
        out.push({ type: 'strong', children: parseInline(source.slice(i + 2, end), 'strong') });
        i = end + 2;
        continue;
      }
    }

    if (ch === '[' && inside !== 'link') {
      const m = LINK.exec(source.slice(i));
      if (m) {
        pushText();
        const children = parseInline(m[1], 'link');
        const href = safeHref(m[2]);
        if (href) out.push({ type: 'link', href, children });
        else out.push(...children);
        i += m[0].length;
        continue;
      }
    }

    text += ch;
    i++;
  }
  pushText();
  return mergeText(out);
}

/** The "**" closing a bold run, skipping any inside a code span. */
function closingStrong(source: string, from: number): number {
  for (let i = from; i < source.length; i++) {
    if (source[i] === '`') {
      const end = source.indexOf('`', i + 1);
      if (end > i) { i = end; continue; }
    }
    if (source[i] === '*' && source[i + 1] === '*') return i;
  }
  return -1;
}

function mergeText(list: Inline[]): Inline[] {
  const out: Inline[] = [];
  for (const seg of list) {
    const last = out[out.length - 1];
    if (seg.type === 'text' && last?.type === 'text') last.text += seg.text;
    else out.push(seg);
  }
  return out;
}

// ── Pages ───────────────────────────────────────────────────────────────────

/**
 * One screen of the dialog. After an update that skipped releases, the first
 * page holds all of them together — what changed since the version the user
 * had — and every older release follows on a page of its own.
 */
export interface Page {
  key: string;
  entries: ChangelogEntry[];
  /** Set on the combined page: the version updated from ("" if unknown). */
  since?: string;
  combined: boolean;
}

/**
 * Arrange the releases into pages, newest first. `fresh` lists the releases
 * new since the last launch; more than one of them become the combined page.
 */
export function buildPages(entries: ChangelogEntry[], fresh: string[] = [], since = ''): Page[] {
  const freshSet = new Set(fresh);
  // A lone new release still says what it was updated from.
  const single = (e: ChangelogEntry): Page => ({
    key: e.version, entries: [e], combined: false,
    ...(since && freshSet.has(e.version) ? { since } : {}),
  });
  const freshEntries = entries.filter((e) => freshSet.has(e.version));
  if (freshEntries.length < 2) return entries.map(single);
  return [
    { key: 'new', entries: freshEntries, since, combined: true },
    ...entries.filter((e) => !freshSet.has(e.version)).map(single),
  ];
}

/** The page to open on: the combined one, else the given version's, else the newest. */
export function startPage(pages: Page[], version: string): number {
  if (pages[0]?.combined) return 0;
  const at = pages.findIndex((p) => p.entries[0]?.version === version);
  return at >= 0 ? at : 0;
}

/** Step towards older (+1) or newer (-1) releases, stopping at either end. */
export function stepPage(index: number, delta: number, count: number): number {
  if (count <= 0) return 0;
  return Math.min(count - 1, Math.max(0, index + delta));
}
