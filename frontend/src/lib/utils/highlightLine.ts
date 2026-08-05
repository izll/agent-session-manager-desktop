import { highlightTree, tags } from '@lezer/highlight';
import { HighlightStyle } from '@codemirror/language';
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
 * The palette.
 *
 * Built with HighlightStyle rather than by matching tags by hand, because tags
 * form a hierarchy that hand-matching gets wrong: a grammar emits
 * `definitionKeyword` or `controlKeyword`, not the plain `keyword` those
 * inherit from, so a list checked with includes() colours almost nothing. Go
 * was the clearest case — `func`, `:=` and every identifier came out plain.
 *
 * HighlightStyle resolves that inheritance, so a rule for `keyword` catches all
 * of its specialisations, and a more specific rule still wins where one exists.
 *
 * The hues match the file browser's theme, so the same code does not change
 * colour depending on where it is being read.
 */
const style = HighlightStyle.define([
  { tag: tags.keyword, color: '#c678dd' },
  { tag: tags.controlKeyword, color: '#c678dd' },
  { tag: tags.moduleKeyword, color: '#c678dd' },
  { tag: tags.definitionKeyword, color: '#c678dd' },
  { tag: tags.operatorKeyword, color: '#c678dd' },
  { tag: tags.comment, color: '#7f848e', fontStyle: 'italic' },
  { tag: tags.string, color: '#98c379' },
  { tag: tags.special(tags.string), color: '#98c379' },
  { tag: tags.number, color: '#d19a66' },
  { tag: tags.bool, color: '#d19a66' },
  { tag: tags.atom, color: '#d19a66' },
  { tag: tags.null, color: '#d19a66' },
  { tag: tags.typeName, color: '#e5c07b' },
  { tag: tags.className, color: '#e5c07b' },
  { tag: tags.namespace, color: '#e5c07b' },
  { tag: tags.function(tags.variableName), color: '#61afef' },
  { tag: tags.function(tags.propertyName), color: '#61afef' },
  { tag: tags.propertyName, color: '#e06c75' },
  { tag: tags.variableName, color: '#e06c75' },
  { tag: tags.attributeName, color: '#d19a66' },
  { tag: tags.tagName, color: '#e06c75' },
  { tag: tags.constant(tags.variableName), color: '#d19a66' },
  { tag: tags.labelName, color: '#61afef' },
  { tag: tags.operator, color: '#56b6c2' },
  { tag: tags.derefOperator, color: '#56b6c2' },
  { tag: tags.definitionOperator, color: '#56b6c2' },
  { tag: tags.regexp, color: '#56b6c2' },
  { tag: tags.escape, color: '#56b6c2' },
  { tag: tags.url, color: '#56b6c2' },
  { tag: tags.link, color: '#56b6c2' },
  { tag: tags.meta, color: '#7f848e' },
  { tag: tags.annotation, color: '#d19a66' },
  { tag: tags.modifier, color: '#c678dd' },
  { tag: tags.self, color: '#d19a66' },
  { tag: tags.heading, color: '#e06c75', fontWeight: 'bold' },
  { tag: tags.strong, fontWeight: 'bold' },
  { tag: tags.emphasis, fontStyle: 'italic' },
  { tag: tags.strikethrough, textDecoration: 'line-through' },
  { tag: tags.invalid, color: '#ff5370' },
]);

/**
 * HighlightStyle emits class names and a stylesheet to go with them. The diff
 * has no stylesheet, so the declarations are read out of that module once and
 * turned into inline style — the mapping is fixed, so it is built eagerly and
 * never recomputed.
 */
const styleByClass = (() => {
  const map = new Map<string, string>();
  const rules = style.module?.getRules() ?? '';
  for (const match of rules.matchAll(/\.(\S+?)\s*\{([^}]*)\}/g)) {
    map.set(match[1], match[2].trim().replace(/;\s*$/, ''));
  }
  return map;
})();

const highlighter = {
  style: (tagList: readonly any[]) => style.style(tagList),
};

/** Resolve the class names HighlightStyle hands back to inline declarations. */
function inlineStyle(classes: string): string {
  const out: string[] = [];
  for (const cls of classes.split(' ')) {
    const rule = styleByClass.get(cls);
    if (rule) out.push(rule);
  }
  return out.join(';');
}

/**
 * The diff marker in a column of its own, followed by a space.
 *
 * Dimmed because it is notation rather than content: it says what happened to
 * the line, and at full strength it competes with the code for attention on
 * every single row.
 */
function markerColumn(marker: string): string {
  if (!marker) return '';
  return `<span style="opacity:0.45">${escapeHtml(marker)}</span> `;
}

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
    // Same column treatment on the unhighlighted path, or plain lines would sit
    // one character off from the coloured ones around them.
    const lead = text[0] === '+' || text[0] === '-' || text[0] === ' ' ? text[0] : '';
    return lead ? markerColumn(lead) + escapeHtml(text.slice(1)) : escapeHtml(text);
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
      const css = style ? inlineStyle(style) : '';
      html += css ? `<span style="${css}">${body}</span>` : body;
      pos = to;
    });

    if (pos < code.length) html += escapeHtml(code.slice(pos));
    // The marker gets its own column, so the code starts at the same place on
    // every line and a change does not appear indented by one character
    // relative to the context around it.
    return markerColumn(marker) + html;
  } catch {
    return escapeHtml(text);
  }
}
