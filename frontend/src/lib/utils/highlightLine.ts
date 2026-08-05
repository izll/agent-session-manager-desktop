import { highlightTree, tags, type Highlighter } from '@lezer/highlight';
import type { LanguageSupport } from '@codemirror/language';

/**
 * Syntax colouring for text rendered outside an editor — the diff view.
 *
 * The file browser gets its colours from a real CodeMirror instance. A diff is
 * not a document: it is fragments of one, interleaved with removed lines that
 * are not in the file at all, so there is nothing coherent to parse as a whole.
 *
 * Each line is parsed on its own instead. That is the compromise this makes:
 * constructs spanning several lines — a block comment, a template literal —
 * are coloured only on the line where they open. Getting that right would mean
 * reconstructing both sides of the file and tracking parser state across hunks,
 * for a view whose job is showing what changed. Wrong colouring inside a
 * comment is a cosmetic flaw; a diff that is slow to open is not.
 */

/**
 * The palette, as inline styles rather than class names.
 *
 * A Highlighter maps syntax tags to whatever string the caller wants; CodeMirror
 * uses class names because it ships a stylesheet. Here the output goes straight
 * into a style attribute, so the declaration is the "class". That avoids
 * shipping a second stylesheet whose names could drift from these tags.
 *
 * The hues match the file browser's theme, so the same code does not change
 * colour depending on where it is being read.
 */
const PALETTE: Array<{ tag: any; style: string }> = [
  { tag: tags.keyword, style: 'color:#c678dd' },
  { tag: [tags.name, tags.deleted, tags.character, tags.macroName], style: 'color:#e06c75' },
  { tag: tags.propertyName, style: 'color:#61afef' },
  { tag: [tags.processingInstruction, tags.string, tags.inserted, tags.special(tags.string)], style: 'color:#98c379' },
  { tag: [tags.function(tags.variableName), tags.labelName], style: 'color:#61afef' },
  { tag: [tags.color, tags.constant(tags.name), tags.standard(tags.name)], style: 'color:#d19a66' },
  { tag: tags.className, style: 'color:#e5c07b' },
  { tag: [tags.number, tags.changed, tags.annotation, tags.modifier, tags.self, tags.namespace], style: 'color:#d19a66' },
  { tag: tags.typeName, style: 'color:#e5c07b' },
  { tag: [tags.operator, tags.operatorKeyword], style: 'color:#56b6c2' },
  { tag: [tags.url, tags.escape, tags.regexp, tags.link], style: 'color:#56b6c2' },
  { tag: [tags.meta, tags.comment], style: 'color:#7f848e;font-style:italic' },
  { tag: [tags.atom, tags.bool, tags.special(tags.variableName)], style: 'color:#d19a66' },
  { tag: tags.invalid, style: 'color:#ff5370' },
];

/** Builds the tag → style lookup once. */
const highlighter: Highlighter = {
  style: (tagList) => {
    for (const entry of PALETTE) {
      const wanted = Array.isArray(entry.tag) ? entry.tag : [entry.tag];
      if (tagList.some((t) => wanted.includes(t))) return entry.style;
    }
    return '';
  },
};

/** HTML-escape, because the result is inserted with {@html}. */
function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

/** Longer than this and the line is data, not code worth colouring: a minified
 *  bundle on one line can be megabytes, and parsing it blocks everything after. */
const MAX_LINE_LENGTH = 2000;

/**
 * Colour one line, returning HTML.
 *
 * Falls back to the escaped text unchanged when there is no grammar, when the
 * line is too long to be worth parsing, or when the parser throws — a line
 * rendered plainly is a far better outcome than a diff that fails to display.
 *
 * Everything reaching {@html} passes through escapeHtml first: diff content is
 * whatever the repository contains, which includes files that are themselves
 * HTML, and a file under review is exactly the wrong place to trust.
 */
export function highlightLine(text: string, language: LanguageSupport | null): string {
  if (!language || !text.trim() || text.length > MAX_LINE_LENGTH) {
    return escapeHtml(text);
  }

  // The +/- a diff puts in column one is not part of the code, and handing it
  // to the parser changes what the rest of the line means: "+func main() {"
  // reads as an addition operator followed by a function, and the colouring
  // slides along with it. Split it off, colour the code, put it back.
  const marker = text[0] === '+' || text[0] === '-' || text[0] === ' ' ? text[0] : '';
  const code = marker ? text.slice(1) : text;
  if (!code.trim()) return escapeHtml(text);

  try {
    const tree = language.language.parser.parse(code);
    let html = '';
    let pos = 0;

    highlightTree(tree, highlighter, (from, to, style) => {
      if (from > pos) html += escapeHtml(code.slice(pos, from));
      const body = escapeHtml(code.slice(from, to));
      html += style ? `<span style="${style}">${body}</span>` : body;
      pos = to;
    });

    if (pos < code.length) html += escapeHtml(code.slice(pos));
    return escapeHtml(marker) + html;
  } catch {
    return escapeHtml(text);
  }
}
