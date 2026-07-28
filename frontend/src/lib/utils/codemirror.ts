/**
 * CodeMirror 6 setup for the file browser's read and edit views.
 *
 * WHY both views share this module: reading and editing must look pixel-
 * identical, and the only way to guarantee that is for both to be the same
 * editor with `readOnly` flipped, drawing from one theme and one highlight
 * style. Two renderers kept in visual sync by hand drift the moment either is
 * touched.
 *
 * WHY the grammars load lazily: twelve Lezer parsers are ~390 KB, and a session
 * typically opens files of one or two types. `loadLanguage` fetches a grammar
 * the first time a file of that type is opened and caches it; startup parses
 * none of them.
 */

import { EditorState, Compartment, type Extension } from '@codemirror/state';
import { EditorView, lineNumbers, highlightActiveLineGutter, keymap } from '@codemirror/view';
import { HighlightStyle, syntaxHighlighting, type LanguageSupport } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import { LogFrontend } from '../../../wailsjs/go/main/App';
import { detectLanguage, type Language } from './highlight';

/**
 * The document's line separator, pinned to LF.
 *
 * NOT optional, and the single most important line in this file. By default
 * `EditorState` splits a document on \r\n, \r, \n AND the Unicode separators
 * U+2028/U+2029, then rejoins every line with one normalised break — so
 * "a\r\nb\nc" silently becomes "a\nb\nc" the moment the file is opened, before
 * the user types anything.
 *
 * session/file_edit.go's decodeForEdit deliberately hands mixed-ending files
 * through RAW, carrying their CRs inside the text, precisely so encodeFromEdit
 * can put them back where they were. Letting CM6 normalise would destroy those
 * CRs and make an unmodified open+save rewrite the file. Pinning the separator
 * to '\n' disables both the CR splitting and the Unicode-separator handling, so
 * the document holds exactly the characters Go sent. Proven in codemirror.test.mjs.
 */
export const lineSeparatorLF: Extension = EditorState.lineSeparator.of('\n');

/**
 * Syntax colours, carried over verbatim from highlight.ts's palette so the CM6
 * views match what the hand-rolled highlighter drew.
 *
 * SEMANTIC and fixed — deliberately not derived from --accent. The accent is
 * user-chosen and can be any hue, so a palette that tracked it would collide
 * with it on some settings.
 *
 * Contrast measured against the pane background (#0a0a0f), per WCAG:
 *
 *   comment  #6b7f8f   4.76:1     function  #82aaff   8.60:1
 *   string   #9ece6a  10.81:1     property  #73daca  11.86:1
 *   number   #ff9e64   9.71:1     tag       #f7768e   7.46:1
 *   keyword  #c792ea   8.21:1     attr      #e0af68   9.88:1
 *   type     #7dcfff  11.51:1     heading   #7aa2f7   7.84:1
 *   literal  #ffc777  12.87:1     punct     #89ddff  13.03:1
 *
 * All clear AA (4.5:1). Comments are the dimmest on purpose — they should
 * recede — but still pass.
 */
const COLOUR = {
  comment: '#6b7f8f',
  string: '#9ece6a',
  number: '#ff9e64',
  keyword: '#c792ea',
  type: '#7dcfff',
  literal: '#ffc777',
  function: '#82aaff',
  property: '#73daca',
  tag: '#f7768e',
  attr: '#e0af68',
  heading: '#7aa2f7',
  punct: '#89ddff',
} as const;

/** Body text, matching .editor-textarea and .file-line code. */
const FOREGROUND = '#d4d4d8';
/** Gutter numbers, matching .line-no and .editor-gutter. */
const GUTTER_FG = '#3f3f46';

/**
 * Lezer tags mapped onto the twelve colours above.
 *
 * Lezer's tag vocabulary is finer-grained than the twelve kinds the old
 * tokeniser emitted, so several tags fold onto one colour — that is the point,
 * since the colours are what the user learned to read.
 */
const highlightStyle = HighlightStyle.define([
  { tag: [tags.comment, tags.lineComment, tags.blockComment, tags.docComment], color: COLOUR.comment, fontStyle: 'italic' },
  { tag: [tags.string, tags.special(tags.string), tags.docString], color: COLOUR.string },
  { tag: [tags.number, tags.integer, tags.float], color: COLOUR.number },
  { tag: [tags.keyword, tags.controlKeyword, tags.moduleKeyword, tags.definitionKeyword, tags.operatorKeyword, tags.modifier, tags.self], color: COLOUR.keyword },
  { tag: [tags.typeName, tags.className, tags.namespace, tags.standard(tags.typeName)], color: COLOUR.type },
  { tag: [tags.bool, tags.null, tags.atom, tags.constant(tags.name), tags.standard(tags.name)], color: COLOUR.literal },
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName), tags.macroName, tags.labelName], color: COLOUR.function },
  { tag: [tags.propertyName, tags.definition(tags.propertyName), tags.attributeValue], color: COLOUR.property },
  { tag: [tags.tagName, tags.angleBracket, tags.documentMeta], color: COLOUR.tag },
  { tag: [tags.attributeName, tags.annotation, tags.meta], color: COLOUR.attr },
  { tag: [tags.heading, tags.heading1, tags.heading2, tags.heading3, tags.heading4, tags.heading5, tags.heading6], color: COLOUR.heading, fontWeight: '600' },
  { tag: [tags.punctuation, tags.separator, tags.bracket, tags.operator, tags.derefOperator], color: COLOUR.punct },
  // Markdown emphasis has no colour of its own in the old palette; keeping the
  // weight/slant alone matches how the read view drew it.
  { tag: tags.strong, fontWeight: '600' },
  { tag: tags.emphasis, fontStyle: 'italic' },
  { tag: tags.link, color: COLOUR.type, textDecoration: 'underline' },
  { tag: tags.invalid, color: '#f87171' },
]);

/**
 * The editor chrome: background, gutter, caret, selection.
 *
 * Values are taken from FileBrowser.svelte's existing rules rather than from
 * One Dark, so the CM6 views drop into the pane without a seam.
 */
const theme = EditorView.theme(
  {
    '&': {
      height: '100%',
      color: FOREGROUND,
      backgroundColor: 'transparent',
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      fontSize: '13px',
    },
    '.cm-scroller': {
      fontFamily: 'inherit',
      lineHeight: '1.6',
      overflow: 'auto',
    },
    '.cm-content': {
      padding: '12px 0',
      caretColor: 'var(--accent)',
    },
    '.cm-line': { padding: '0 16px' },
    '.cm-gutters': {
      backgroundColor: 'rgba(0, 0, 0, 0.2)',
      color: GUTTER_FG,
      border: 'none',
    },
    '.cm-lineNumbers .cm-gutterElement': { padding: '0 8px 0 12px', minWidth: '44px' },
    '.cm-activeLineGutter': { backgroundColor: 'transparent', color: '#6b7280' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent)' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
      backgroundColor: 'rgba(var(--accent-rgb), 0.25)',
    },
    '.cm-selectionMatch': { backgroundColor: 'rgba(var(--accent-rgb), 0.15)' },
    // The pane's own scrollbars, so the editor matches .file-content.
    '.cm-scroller::-webkit-scrollbar': { width: '6px', height: '6px' },
    '.cm-scroller::-webkit-scrollbar-track': { background: 'transparent' },
    '.cm-scroller::-webkit-scrollbar-thumb': {
      background: 'rgba(var(--accent-rgb), 0.3)',
      borderRadius: '3px',
    },
  },
  { dark: true }
);

/**
 * Extensions both views share.
 *
 * Soft wrap is absent on purpose: CM6 does not wrap unless
 * `EditorView.lineWrapping` is added, and the read and edit views both scroll
 * horizontally instead — matching the textarea this replaced.
 */
export function baseExtensions(): Extension[] {
  return [lineSeparatorLF, lineNumbers(), highlightActiveLineGutter(), syntaxHighlighting(highlightStyle), theme, EditorState.tabSize.of(4)];
}

/**
 * Insert a literal tab.
 *
 * CM6's default Tab binding runs indentation commands, which reindent by the
 * language's rules and can rewrite whitespace the user did not touch. The
 * textarea this replaced inserted one tab character, so this keeps that. Shift+
 * Tab is deliberately left unbound so the editor can still be escaped by
 * keyboard.
 */
export const insertTabKeymap = keymap.of([
  {
    key: 'Tab',
    run: (view) => {
      view.dispatch(view.state.replaceSelection('\t'), { scrollIntoView: true, userEvent: 'input.type' });
      return true;
    },
  },
]);

// --- Lazy language loading -------------------------------------------------

/**
 * The languages we have a Lezer grammar for. Keyed by highlight.ts's `Language`
 * so the filename→language detection is shared rather than duplicated — with
 * two exceptions handled by `grammarFor` below.
 */
type GrammarKey = 'go' | 'js' | 'python' | 'rust' | 'json' | 'yaml' | 'markdown' | 'shell' | 'sql' | 'html' | 'css';

/**
 * Loaders, one dynamic import each. Rollup turns every one into its own chunk,
 * which is what makes the loading lazy: opening a Go file fetches go.js and
 * nothing else.
 */
const LOADERS: Record<GrammarKey, () => Promise<LanguageSupport>> = {
  go: () => import('@codemirror/lang-go').then((m) => m.go()),
  js: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ typescript: true, jsx: true })),
  python: () => import('@codemirror/lang-python').then((m) => m.python()),
  rust: () => import('@codemirror/lang-rust').then((m) => m.rust()),
  json: () => import('@codemirror/lang-json').then((m) => m.json()),
  yaml: () => import('@codemirror/lang-yaml').then((m) => m.yaml()),
  markdown: () => import('@codemirror/lang-markdown').then((m) => m.markdown()),
  sql: () => import('@codemirror/lang-sql').then((m) => m.sql()),
  html: () => import('@codemirror/lang-html').then((m) => m.html()),
  css: () => import('@codemirror/lang-css').then((m) => m.css()),
  // Shell has no lang-* package; the legacy StreamLanguage mode is the
  // supported route and is a fraction of the size of a Lezer grammar.
  shell: () =>
    Promise.all([import('@codemirror/language'), import('@codemirror/legacy-modes/mode/shell')]).then(
      ([lang, mode]) => new lang.LanguageSupport(lang.StreamLanguage.define(mode.shell))
    ),
};

/**
 * Map highlight.ts's detected language onto a grammar we can load.
 *
 * Reusing `detectLanguage` rather than writing a second filename table is
 * deliberate — one mapping cannot drift from the other. Two of its outputs have
 * no CM6 equivalent:
 *
 *  - 'toml' has no @codemirror/lang-toml, so .toml files render plain rather
 *    than pulling in a legacy mode for one format.
 *  - Svelte and Vue already map to 'html' inside detectLanguage, which is
 *    exactly the requirement, so they need nothing here.
 */
function grammarFor(lang: Language | null): GrammarKey | null {
  if (!lang || lang === 'toml') return null;
  return lang;
}

/**
 * Grammars already loaded, so reopening a file of a known type is synchronous
 * and the second open of a Go file costs no fetch.
 */
const cache = new Map<GrammarKey, LanguageSupport>();
/** In-flight loads, so opening two Go files at once fetches the chunk once. */
const inFlight = new Map<GrammarKey, Promise<LanguageSupport | null>>();

/** A grammar already in the cache, or null. Lets a mount skip the async path. */
export function cachedLanguage(path: string): LanguageSupport | null {
  const key = grammarFor(detectLanguage(path));
  return key ? cache.get(key) || null : null;
}

/**
 * Load the grammar for a path, or null when we have none (which is the signal
 * to render plain text). A failed chunk fetch resolves null rather than
 * throwing: an uncoloured file is a far better outcome than a blank pane.
 */
export function loadLanguage(path: string): Promise<LanguageSupport | null> {
  const key = grammarFor(detectLanguage(path));
  if (!key) return Promise.resolve(null);

  const ready = cache.get(key);
  if (ready) return Promise.resolve(ready);

  const pending = inFlight.get(key);
  if (pending) return pending;

  const load = LOADERS[key]()
    .then((support) => {
      cache.set(key, support);
      inFlight.delete(key);
      return support;
    })
    .catch((err) => {
      // Reported, not swallowed. A grammar that fails to load leaves the file
      // unhighlighted, which looks the same as a file we have no grammar for —
      // so without this the failure is invisible and unexplainable.
      // Through LogFrontend, not console.error: console output does not
      // survive the Wails webview, so a failure here would leave no trace.
      void LogFrontend(`[codemirror] failed to load the ${key} grammar: ${err}`);
      inFlight.delete(key);
      return null;
    });
  inFlight.set(key, load);
  return load;
}

/** Which grammars are currently loaded. Exists for the lazy-loading test. */
export function loadedGrammars(): string[] {
  return Array.from(cache.keys()).sort();
}

/** A compartment per view, so the language can be swapped without a rebuild. */
export function languageCompartment(): Compartment {
  return new Compartment();
}
