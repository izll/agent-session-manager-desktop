/**
 * A small syntax highlighter for the file browser's content pane.
 *
 * WHY hand-rolled rather than highlight.js/prism/shiki: this app runs in a
 * WebKitGTK webview that is already known to be slow (CLAUDE.md records that
 * xterm.js's WebGL renderer had to be disabled because it lags here), and the
 * frontend bundle is already ~1.3 MB. highlight.js's core plus a dozen grammars
 * is ~150 KB, shiki brings a WASM regex engine, and neither lets us bound the
 * per-line cost — which is what actually matters when the alternative to a slow
 * highlight is a frozen window.
 *
 * The languages worth covering here (Go, TS/JS, Svelte, Python, Rust, JSON,
 * YAML, TOML, Markdown, shell, SQL, HTML, CSS) share one structural core:
 * comments, strings, numbers, keywords, punctuation. One table-driven scanner
 * over that core costs a few KB and is far easier to keep correct than a dozen
 * imported grammars.
 *
 * The trade is coverage, not correctness: this deliberately does not attempt
 * semantic analysis, template-literal nesting, or regex-vs-division
 * disambiguation. Anything it is unsure of stays plain. What it must never do
 * is alter the file's characters — see `tokenize`'s invariant and the tests.
 */

/** A slice of a line and what it is. `null` means "no colour". */
export interface Token {
  text: string;
  kind: TokenKind | null;
}

export type TokenKind =
  | 'comment'
  | 'string'
  | 'number'
  | 'keyword'
  | 'type'
  | 'literal'
  | 'function'
  | 'property'
  | 'tag'
  | 'attr'
  | 'heading'
  | 'punct';

/** The languages we can colour. Anything else renders plain. */
export type Language =
  | 'go'
  | 'js'
  | 'python'
  | 'rust'
  | 'json'
  | 'yaml'
  | 'toml'
  | 'markdown'
  | 'shell'
  | 'sql'
  | 'html'
  | 'css'
  // The C-family languages share one scanner (see LOADERS): the differences
  // between them are in their keyword sets, which the mode is parameterised
  // by, not in how they are tokenised.
  | 'kotlin'
  | 'java'
  | 'csharp'
  | 'cpp'
  | 'c'
  | 'scala'
  | 'dart'
  | 'php'
  | 'ruby'
  | 'lua'
  | 'swift';

// --- Language detection ---------------------------------------------------

// NOTE: this deliberately does NOT reuse fileTypes.ts's mapping, even though
// both start from a filename. That module answers "which colour family does
// this row get", and groups by what a developer treats as interchangeable —
// its `web` family covers TS, JS, HTML and Svelte together, and `data` covers
// JSON, YAML and TOML. A tokeniser has to tell those apart: they need
// different scanners. The two tables answer different questions, so sharing one
// would mean weakening the file-list grouping to serve the highlighter.

const EXTENSIONS: Record<string, Language> = {
  go: 'go',
  mod: 'go', // go.mod is close enough to Go's own comment/string rules
  sum: 'go',
  ts: 'js',
  tsx: 'js',
  js: 'js',
  jsx: 'js',
  mjs: 'js',
  cjs: 'js',
  mts: 'js',
  cts: 'js',
  // Svelte and Vue files are mostly script and style with markup around them.
  // The HTML scanner handles the markup and hands embedded <script>/<style>
  // bodies to the JS and CSS scanners, which is what makes them readable.
  svelte: 'html',
  vue: 'html',
  html: 'html',
  htm: 'html',
  xml: 'html',
  svg: 'html',
  css: 'css',
  scss: 'css',
  sass: 'css',
  less: 'css',
  py: 'python',
  pyi: 'python',
  rs: 'rust',
  json: 'json',
  jsonc: 'json',
  yaml: 'yaml',
  yml: 'yaml',
  toml: 'toml',
  md: 'markdown',
  markdown: 'markdown',
  sh: 'shell',
  bash: 'shell',
  zsh: 'shell',
  fish: 'shell',
  ksh: 'shell',
  sql: 'sql',
  kt: 'kotlin',
  kts: 'kotlin',
  java: 'java',
  cs: 'csharp',
  cpp: 'cpp',
  cxx: 'cpp',
  cc: 'cpp',
  hpp: 'cpp',
  hxx: 'cpp',
  c: 'c',
  h: 'c',
  scala: 'scala',
  sc: 'scala',
  dart: 'dart',
  php: 'php',
  rb: 'ruby',
  rake: 'ruby',
  gemfile: 'ruby',
  lua: 'lua',
  swift: 'swift',
};

/** Files whose whole name decides the language, extension or not. */
const FILENAMES: Record<string, Language> = {
  dockerfile: 'shell',
  makefile: 'shell',
  '.bashrc': 'shell',
  '.zshrc': 'shell',
  '.profile': 'shell',
  '.bash_profile': 'shell',
  '.gitignore': 'shell', // # comments; close enough to read
  '.dockerignore': 'shell',
  '.npmrc': 'toml',
  '.editorconfig': 'toml',
  'go.mod': 'go',
  'go.sum': 'go',
  'cargo.toml': 'toml',
  'cargo.lock': 'toml',
};

/**
 * Pick a language from a path. Returns null when we have no grammar for it,
 * which is the signal to render plain text.
 */
export function detectLanguage(path: string): Language | null {
  if (!path) return null;
  const slash = path.lastIndexOf('/');
  const name = (slash < 0 ? path : path.slice(slash + 1)).toLowerCase();
  if (!name) return null;

  const byName = FILENAMES[name];
  if (byName) return byName;

  const dot = name.lastIndexOf('.');
  // A leading dot is the whole name (".env"), not an extension.
  if (dot <= 0) return null;
  return EXTENSIONS[name.slice(dot + 1)] || null;
}

// --- Keyword tables -------------------------------------------------------

/** Built once at module load; Set lookup beats a regex alternation per word. */
const set = (words: string): Set<string> => new Set(words.split(' '));

interface Grammar {
  keywords: Set<string>;
  /** Types and built-ins — coloured apart from control-flow keywords. */
  types: Set<string>;
  /** true/false/nil/None and friends. */
  literals: Set<string>;
  lineComment: string[];
  blockComment?: [string, string];
  /** Quote characters that open a string. */
  quotes: string[];
  /** Whether a backslash escapes the next character inside a string. */
  escapes: boolean;
}

const GO: Grammar = {
  keywords: set(
    'break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'
  ),
  types: set(
    'bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any append cap close copy delete len make new panic print println recover'
  ),
  literals: set('true false nil iota'),
  lineComment: ['//'],
  blockComment: ['/*', '*/'],
  quotes: ['"', '`', "'"],
  escapes: true,
};

const JS: Grammar = {
  keywords: set(
    'abstract as async await break case catch class const continue debugger declare default delete do else enum export extends finally for from function get if implements import in infer instanceof interface is keyof let namespace new of package private protected public readonly return satisfies set static super switch this throw try type typeof var void while with yield'
  ),
  types: set(
    'any bigint boolean never number object string symbol unknown Array Boolean Date Error Function JSON Map Math Number Object Promise Proxy Reflect RegExp Set String Symbol WeakMap WeakSet console document window globalThis'
  ),
  literals: set('true false null undefined NaN Infinity'),
  lineComment: ['//'],
  blockComment: ['/*', '*/'],
  quotes: ['"', "'", '`'],
  escapes: true,
};

const PYTHON: Grammar = {
  keywords: set(
    'and as assert async await break class continue def del elif else except finally for from global if import in is lambda match nonlocal not or pass raise return try while with yield'
  ),
  types: set(
    'bool bytes dict float frozenset int list object set str tuple type abs all any enumerate filter format isinstance len map max min open print range repr reversed sorted sum super zip self cls'
  ),
  literals: set('True False None Ellipsis NotImplemented'),
  lineComment: ['#'],
  quotes: ['"', "'"],
  escapes: true,
};

const RUST: Grammar = {
  keywords: set(
    'as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait type unsafe use where while'
  ),
  types: set(
    'bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize String Vec Option Result Box Rc Arc HashMap HashSet Some None Ok Err'
  ),
  literals: set('true false'),
  lineComment: ['//'],
  blockComment: ['/*', '*/'],
  quotes: ['"', "'"],
  escapes: true,
};

const SQL: Grammar = {
  keywords: set(
    'add all alter and as asc begin between by case cast column commit constraint create cross default delete desc distinct drop else end exists foreign from full group having if in index inner insert into is join key left like limit not null offset on or order outer primary references replace return returning right rollback select set table then transaction union unique update values view when where with'
  ),
  types: set(
    'bigint blob boolean bytea char date datetime decimal double float int integer json jsonb numeric real serial smallint text time timestamp uuid varchar avg count max min sum coalesce now'
  ),
  literals: set('true false null'),
  lineComment: ['--', '#'],
  blockComment: ['/*', '*/'],
  quotes: ["'", '"', '`'],
  escapes: true,
};

const SHELL: Grammar = {
  keywords: set(
    'alias break case cd continue declare do done elif else esac eval exec exit export fi for function if in local read return set shift source then time trap unset until while'
  ),
  types: set(
    'awk cat chmod chown cp curl cut date df du echo find git grep head kill ln ls make mkdir mv printf ps pwd rm rmdir sed sleep sort tail tar tee touch tr uniq wc wget xargs'
  ),
  literals: set('true false'),
  lineComment: ['#'],
  quotes: ['"', "'"],
  escapes: true,
};

const CSS: Grammar = {
  keywords: set(''),
  types: set(''),
  literals: set(''),
  lineComment: ['//'], // valid in scss/less, harmless in plain css
  blockComment: ['/*', '*/'],
  quotes: ['"', "'"],
  escapes: true,
};

const JSON_G: Grammar = {
  keywords: set(''),
  types: set(''),
  literals: set('true false null'),
  lineComment: ['//'], // jsonc
  blockComment: ['/*', '*/'],
  quotes: ['"'],
  escapes: true,
};

const GRAMMARS: Record<string, Grammar> = {
  go: GO,
  js: JS,
  python: PYTHON,
  rust: RUST,
  sql: SQL,
  shell: SHELL,
  css: CSS,
  json: JSON_G,
};

// --- Character classes ----------------------------------------------------

function isDigit(c: string): boolean {
  return c >= '0' && c <= '9';
}

/**
 * Identifier characters. Non-ASCII is included wholesale: this only decides
 * where a word ends, and treating `ünicode` as one word is right for every
 * language here.
 */
function isIdentStart(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c === '_' || c === '$' || c > '\x7f';
}

function isIdent(c: string): boolean {
  return isIdentStart(c) || isDigit(c);
}

// --- Scanner state --------------------------------------------------------

/**
 * What a line inherits from the one before it. Highlighting is line-by-line
 * (the pane renders one element per line), so multi-line constructs are carried
 * across in this value rather than by re-scanning from the top of the file.
 */
export interface ScanState {
  /** Inside a /* *​/-style block comment. */
  block: boolean;
  /** Inside a multi-line string, holding the quote that opened it. */
  quote: string | null;
  /** For embedded languages in HTML: which scanner owns the current line. */
  embedded: 'js' | 'css' | null;
}

export function initialState(): ScanState {
  return { block: false, quote: null, embedded: null };
}

/** The shared scanner's result for one line. */
interface LineResult {
  tokens: Token[];
  state: ScanState;
}

// --- Generic C-family / scripting scanner ---------------------------------

function scanGeneric(line: string, grammar: Grammar, state: ScanState): LineResult {
  const tokens: Token[] = [];
  const n = line.length;
  let i = 0;
  // Everything since the last emitted token that has no colour of its own.
  let plainStart = 0;

  const flushPlain = (end: number) => {
    if (end > plainStart) tokens.push({ text: line.slice(plainStart, end), kind: null });
  };
  const emit = (start: number, end: number, kind: TokenKind) => {
    flushPlain(start);
    tokens.push({ text: line.slice(start, end), kind });
    plainStart = end;
  };

  let block = state.block;
  let quote = state.quote;

  // A line that starts inside a block comment: consume up to the closer.
  if (block && grammar.blockComment) {
    const close = line.indexOf(grammar.blockComment[1]);
    if (close < 0) {
      return { tokens: [{ text: line, kind: 'comment' }], state: { ...state, block: true } };
    }
    const end = close + grammar.blockComment[1].length;
    tokens.push({ text: line.slice(0, end), kind: 'comment' });
    plainStart = end;
    i = end;
    block = false;
  }

  // A line that starts inside a multi-line string (Go's backticks, Python's
  // triple quotes): consume up to the matching closer.
  if (quote) {
    const end = findStringEnd(line, i, quote, grammar.escapes);
    if (end < 0) {
      flushPlain(i);
      tokens.push({ text: line.slice(i), kind: 'string' });
      return { tokens, state: { ...state, block, quote } };
    }
    emit(i, end, 'string');
    i = end;
    quote = null;
  }

  while (i < n) {
    const c = line[i];

    // Line comment.
    let matchedComment = false;
    for (const marker of grammar.lineComment) {
      if (line.startsWith(marker, i)) {
        emit(i, n, 'comment');
        i = n;
        matchedComment = true;
        break;
      }
    }
    if (matchedComment) break;

    // Block comment.
    if (grammar.blockComment && line.startsWith(grammar.blockComment[0], i)) {
      const close = line.indexOf(grammar.blockComment[1], i + grammar.blockComment[0].length);
      if (close < 0) {
        emit(i, n, 'comment');
        i = n;
        block = true;
        break;
      }
      const end = close + grammar.blockComment[1].length;
      emit(i, end, 'comment');
      i = end;
      continue;
    }

    // String.
    if (grammar.quotes.indexOf(c) >= 0) {
      const opener = tripleQuote(line, i, c) ? line.slice(i, i + 3) : c;
      const end = findStringEnd(line, i + opener.length, opener, grammar.escapes);
      if (end < 0) {
        emit(i, n, 'string');
        i = n;
        // Only quotes that legitimately span lines carry over; a plain "…"
        // left open is far more likely a quote inside prose than a real
        // multi-line string, and carrying it would mis-colour the rest of the
        // file.
        quote = opener.length === 3 || opener === '`' ? opener : null;
        break;
      }
      emit(i, end, 'string');
      i = end;
      continue;
    }

    // Number. Deliberately loose: covers 0x1f, 1_000, 1.5e-3, 42u8, 3.14f64.
    if (isDigit(c) || (c === '.' && isDigit(line[i + 1] || ''))) {
      let j = i;
      while (j < n && (isIdent(line[j]) || line[j] === '.')) {
        // An exponent's sign is part of the number, but only right after e/E.
        j++;
        if (
          j < n &&
          (line[j] === '+' || line[j] === '-') &&
          (line[j - 1] === 'e' || line[j - 1] === 'E')
        ) {
          j++;
        }
      }
      emit(i, j, 'number');
      i = j;
      continue;
    }

    // Word: keyword, type, literal, function call, or plain identifier.
    if (isIdentStart(c)) {
      let j = i;
      while (j < n && isIdent(line[j])) j++;
      const word = line.slice(i, j);
      if (grammar.keywords.has(word)) {
        emit(i, j, 'keyword');
      } else if (grammar.literals.has(word)) {
        emit(i, j, 'literal');
      } else if (grammar.types.has(word)) {
        emit(i, j, 'type');
      } else if (line[j] === '(') {
        // A word immediately before "(" reads as a call at a glance, which is
        // the only thing colouring it is for.
        emit(i, j, 'function');
      }
      i = j;
      continue;
    }

    i++;
  }

  flushPlain(n);
  return { tokens, state: { block, quote, embedded: state.embedded } };
}

/** Is there a triple quote (Python's """/''') starting at `i`? */
function tripleQuote(line: string, i: number, c: string): boolean {
  return (c === '"' || c === "'") && line[i + 1] === c && line[i + 2] === c;
}

/**
 * Index just past the closing quote, or -1 if the string does not close on this
 * line. `from` is the first character of the string's body.
 */
function findStringEnd(line: string, from: number, opener: string, escapes: boolean): number {
  const n = line.length;
  // A raw string (Go's backticks) has no escapes, so a backslash is literal.
  const escaping = escapes && opener !== '`';
  let i = from;
  while (i < n) {
    if (escaping && line[i] === '\\') {
      i += 2;
      continue;
    }
    if (line.startsWith(opener, i)) return i + opener.length;
    i++;
  }
  return -1;
}

// --- Structured formats ---------------------------------------------------

/**
 * JSON, plus jsonc comments. Keys are coloured apart from string values, which
 * is most of the value of highlighting a config file.
 */
function scanJson(line: string, state: ScanState): LineResult {
  const base = scanGeneric(line, JSON_G, state);
  // A string token followed only by whitespace and ":" is a key.
  const tokens = base.tokens;
  for (let k = 0; k < tokens.length; k++) {
    if (tokens[k].kind !== 'string') continue;
    const after = tokens[k + 1];
    if (after && after.kind === null && /^\s*:/.test(after.text)) tokens[k].kind = 'property';
  }
  return base;
}

function scanYaml(line: string, state: ScanState): LineResult {
  const tokens: Token[] = [];
  const trimmedStart = line.length - line.replace(/^\s+/, '').length;
  const body = line.slice(trimmedStart);

  if (body.startsWith('#')) return { tokens: [{ text: line, kind: 'comment' }], state };
  if (body === '---' || body === '...') {
    return { tokens: [{ text: line, kind: 'punct' }], state };
  }

  let rest = trimmedStart;
  if (trimmedStart > 0) tokens.push({ text: line.slice(0, trimmedStart), kind: null });

  // A list marker is structure, not content.
  let afterDash = rest;
  if (line.startsWith('- ', rest) || line.slice(rest) === '-') {
    tokens.push({ text: '-', kind: 'punct' });
    afterDash = rest + 1;
  }

  // "key:" — only when the colon is followed by end-of-line or whitespace, so
  // a URL inside a value isn't mistaken for a key.
  const keyMatch = /^(\s*)([^\s#][^:#]*?)(:)(\s|$)/.exec(line.slice(afterDash));
  if (keyMatch) {
    const start = afterDash + keyMatch[1].length;
    if (keyMatch[1]) tokens.push({ text: keyMatch[1], kind: null });
    tokens.push({ text: keyMatch[2], kind: 'property' });
    tokens.push({ text: ':', kind: 'punct' });
    const valueStart = start + keyMatch[2].length + 1;
    pushYamlValue(tokens, line.slice(valueStart));
    return { tokens, state };
  }

  pushYamlValue(tokens, line.slice(afterDash));
  return { tokens, state };
}

/** Colour a scalar YAML/TOML value: strings, numbers, booleans, comments. */
function pushYamlValue(tokens: Token[], value: string): void {
  if (!value) return;
  const comment = findUnquotedHash(value);
  const body = comment < 0 ? value : value.slice(0, comment);

  if (body) {
    const trimmed = body.trim();
    let kind: TokenKind | null = null;
    if (/^["'].*["']$/.test(trimmed) || /^["']/.test(trimmed)) kind = 'string';
    else if (/^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(trimmed)) kind = 'number';
    else if (/^(true|false|null|yes|no|on|off|~)$/i.test(trimmed)) kind = 'literal';
    tokens.push({ text: body, kind });
  }
  if (comment >= 0) tokens.push({ text: value.slice(comment), kind: 'comment' });
}

/** First "#" that is not inside a quoted run, or -1. */
function findUnquotedHash(s: string): number {
  let quote: string | null = null;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (quote) {
      if (c === '\\') i++;
      else if (c === quote) quote = null;
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === '#') {
      // A "#" glued to a word is part of it (a colour, a fragment), not a
      // comment — YAML requires whitespace before an inline comment.
      if (i === 0 || /\s/.test(s[i - 1])) return i;
    }
  }
  return -1;
}

function scanToml(line: string, state: ScanState): LineResult {
  const tokens: Token[] = [];
  const trimmed = line.trim();
  if (trimmed.startsWith('#')) return { tokens: [{ text: line, kind: 'comment' }], state };
  if (trimmed.startsWith('[')) return { tokens: [{ text: line, kind: 'heading' }], state };

  const eq = line.indexOf('=');
  if (eq < 0) return { tokens: [{ text: line, kind: null }], state };
  tokens.push({ text: line.slice(0, eq), kind: 'property' });
  tokens.push({ text: '=', kind: 'punct' });
  pushYamlValue(tokens, line.slice(eq + 1));
  return { tokens, state };
}

// --- Markdown -------------------------------------------------------------

function scanMarkdown(line: string, state: ScanState): LineResult {
  // A fenced code block is carried across lines in `quote`, reusing the field
  // rather than growing the state object.
  if (state.quote === '```') {
    if (/^\s*```/.test(line)) {
      return { tokens: [{ text: line, kind: 'punct' }], state: { ...state, quote: null } };
    }
    return { tokens: [{ text: line, kind: 'string' }], state };
  }
  if (/^\s*```/.test(line)) {
    return { tokens: [{ text: line, kind: 'punct' }], state: { ...state, quote: '```' } };
  }
  if (/^\s{0,3}#{1,6}\s/.test(line)) {
    return { tokens: [{ text: line, kind: 'heading' }], state };
  }
  if (/^\s{0,3}>/.test(line)) {
    return { tokens: [{ text: line, kind: 'comment' }], state };
  }

  // Inline: `code`, **bold**, [text](link). Anything else stays plain — over-
  // marking prose reads worse than leaving it alone.
  const tokens: Token[] = [];
  const re = /(`[^`]*`)|(\*\*[^*]+\*\*|__[^_]+__)|(\[[^\]]*\]\([^)]*\))/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(line)) !== null) {
    if (m.index > last) tokens.push({ text: line.slice(last, m.index), kind: null });
    tokens.push({ text: m[0], kind: m[1] ? 'string' : m[2] ? 'keyword' : 'type' });
    last = m.index + m[0].length;
  }
  if (last < line.length) tokens.push({ text: line.slice(last), kind: null });

  // A list bullet at the start is worth marking; the rest of the line is above.
  const bullet = /^(\s*)([-*+]|\d+\.)\s/.exec(line);
  if (bullet && tokens.length && tokens[0].kind === null) {
    const head = bullet[1].length + bullet[2].length;
    const first = tokens[0];
    if (first.text.length >= head) {
      tokens.splice(
        0,
        1,
        { text: first.text.slice(0, bullet[1].length), kind: null },
        { text: bullet[2], kind: 'punct' },
        { text: first.text.slice(head), kind: null }
      );
    }
  }
  return { tokens, state };
}

// --- HTML / Svelte --------------------------------------------------------

const EMBED_OPEN = /<(script|style)\b[^>]*>/i;

/**
 * Markup, with <script> and <style> bodies handed to the JS and CSS scanners.
 * That delegation is what makes a .svelte file readable — most of one is
 * script, and colouring only its tags would be close to useless.
 */
function scanHtml(line: string, state: ScanState): LineResult {
  if (state.embedded === 'js' || state.embedded === 'css') {
    const closer = state.embedded === 'js' ? /<\/script\s*>/i : /<\/style\s*>/i;
    const close = closer.exec(line);
    if (!close) {
      const grammar = state.embedded === 'js' ? JS : CSS;
      const inner = scanGeneric(line, grammar, state);
      return { tokens: inner.tokens, state: { ...inner.state, embedded: state.embedded } };
    }
    const head = line.slice(0, close.index);
    const grammar = state.embedded === 'js' ? JS : CSS;
    const inner = head
      ? scanGeneric(head, grammar, state)
      : { tokens: [] as Token[], state: { ...state } };
    const tokens = inner.tokens.slice();
    tokens.push({ text: line.slice(close.index), kind: 'tag' });
    return { tokens, state: { block: false, quote: null, embedded: null } };
  }

  // Inside a <!-- --> comment carried from a previous line.
  if (state.block) {
    const close = line.indexOf('-->');
    if (close < 0) return { tokens: [{ text: line, kind: 'comment' }], state };
    const end = close + 3;
    const rest = scanHtml(line.slice(end), { ...state, block: false });
    return {
      tokens: [{ text: line.slice(0, end), kind: 'comment' }, ...rest.tokens],
      state: rest.state,
    };
  }

  const tokens: Token[] = [];
  const n = line.length;
  let i = 0;
  let plainStart = 0;
  let embedded: ScanState['embedded'] = null;
  let block = false;

  const flushPlain = (end: number) => {
    if (end > plainStart) tokens.push({ text: line.slice(plainStart, end), kind: null });
  };

  while (i < n) {
    if (line.startsWith('<!--', i)) {
      flushPlain(i);
      const close = line.indexOf('-->', i + 4);
      if (close < 0) {
        tokens.push({ text: line.slice(i), kind: 'comment' });
        return { tokens, state: { block: true, quote: null, embedded: null } };
      }
      const end = close + 3;
      tokens.push({ text: line.slice(i, end), kind: 'comment' });
      plainStart = end;
      i = end;
      continue;
    }

    if (line[i] !== '<') {
      i++;
      continue;
    }

    const close = line.indexOf('>', i);
    if (close < 0) {
      // An unterminated tag: colour what we have and stop guessing.
      flushPlain(i);
      tokens.push({ text: line.slice(i), kind: 'tag' });
      plainStart = n;
      i = n;
      break;
    }

    flushPlain(i);
    const end = close + 1;
    pushTagTokens(tokens, line.slice(i, end));
    plainStart = end;

    const embed = EMBED_OPEN.exec(line.slice(i, end));
    if (embed && !line.slice(i, end).startsWith('</')) {
      // Self-closing or immediately-closed tags open nothing.
      const kind = embed[1].toLowerCase() === 'script' ? 'js' : 'css';
      const closer = kind === 'js' ? /<\/script\s*>/i : /<\/style\s*>/i;
      const after = line.slice(end);
      const closeMatch = closer.exec(after);
      if (closeMatch) {
        const inner = after.slice(0, closeMatch.index);
        if (inner) {
          const scanned = scanGeneric(inner, kind === 'js' ? JS : CSS, initialState());
          tokens.push(...scanned.tokens);
        }
        pushTagTokens(tokens, closeMatch[0]);
        plainStart = end + closeMatch.index + closeMatch[0].length;
        i = plainStart;
        continue;
      }
      const rest = after;
      if (rest) {
        const scanned = scanGeneric(rest, kind === 'js' ? JS : CSS, initialState());
        tokens.push(...scanned.tokens);
        block = scanned.state.block;
      }
      return { tokens, state: { block, quote: null, embedded: kind } };
    }

    i = end;
  }

  flushPlain(n);
  return { tokens, state: { block, quote: null, embedded } };
}

/** Split one `<tag attr="value">` into tag / attribute / string tokens. */
function pushTagTokens(tokens: Token[], tag: string): void {
  const re = /([a-zA-Z_:][-\w:.]*)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)|([a-zA-Z_:][-\w:.]*)/g;
  let last = 0;
  let first = true;
  let m: RegExpExecArray | null;
  while ((m = re.exec(tag)) !== null) {
    if (m.index > last) tokens.push({ text: tag.slice(last, m.index), kind: 'tag' });
    if (first) {
      // The element name itself.
      tokens.push({ text: m[0], kind: 'tag' });
      first = false;
    } else if (m[1]) {
      tokens.push({ text: m[1], kind: 'attr' });
      tokens.push({ text: tag.slice(m.index + m[1].length, re.lastIndex - m[2].length), kind: null });
      tokens.push({ text: m[2], kind: 'string' });
    } else {
      tokens.push({ text: m[0], kind: 'attr' });
    }
    last = re.lastIndex;
  }
  if (last < tag.length) tokens.push({ text: tag.slice(last), kind: 'tag' });
}

// --- CSS ------------------------------------------------------------------

function scanCss(line: string, state: ScanState): LineResult {
  const base = scanGeneric(line, CSS, state);
  // A word before ":" inside a declaration is a property. Done as a pass over
  // the generic tokens so the comment and string handling above is shared.
  const tokens: Token[] = [];
  for (const token of base.tokens) {
    if (token.kind !== null) {
      tokens.push(token);
      continue;
    }
    const re = /([-\w]+)(\s*:)/g;
    let last = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(token.text)) !== null) {
      if (m.index > last) tokens.push({ text: token.text.slice(last, m.index), kind: null });
      tokens.push({ text: m[1], kind: 'property' });
      tokens.push({ text: m[2], kind: null });
      last = re.lastIndex;
    }
    if (last < token.text.length) tokens.push({ text: token.text.slice(last), kind: null });
  }
  return { tokens, state: base.state };
}

// --- Entry point ----------------------------------------------------------

/**
 * Tokenise ONE line.
 *
 * INVARIANT, and the one thing that must never break: the tokens' `text`
 * concatenated back together equals `line`, exactly. Every scanner above emits
 * slices of the input and never rewrites them, so the pane always shows the
 * file's real characters. `highlightLines` asserts this per line and falls back
 * to plain text if it ever fails.
 */
export function tokenizeLine(line: string, lang: Language, state: ScanState): LineResult {
  switch (lang) {
    case 'json':
      return scanJson(line, state);
    case 'yaml':
      return scanYaml(line, state);
    case 'toml':
      return scanToml(line, state);
    case 'markdown':
      return scanMarkdown(line, state);
    case 'html':
      return scanHtml(line, state);
    case 'css':
      return scanCss(line, state);
    default:
      return scanGeneric(line, GRAMMARS[lang] || JS, state);
  }
}

/**
 * Above this a single line is left plain. Minified bundles and embedded data
 * URIs arrive as one line of hundreds of kilobytes; the scanners are linear,
 * but the token array and the DOM nodes it becomes are not worth building for a
 * line nobody can read anyway.
 */
export const MAX_HIGHLIGHT_LINE_LENGTH = 2000;

/**
 * Tokenise a list of lines. Returns null when `lang` is null, so callers can
 * treat "no highlighting" as one case rather than checking a flag.
 *
 * The per-line try/catch is not defensive padding: a scanner bug must degrade
 * to plain text for that line, never blank the pane or throw through Svelte's
 * render.
 */
export function highlightLines(lines: string[], lang: Language | null): Token[][] | null {
  if (!lang) return null;
  const out: Token[][] = new Array(lines.length);
  let state = initialState();
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.length > MAX_HIGHLIGHT_LINE_LENGTH) {
      out[i] = [{ text: line, kind: null }];
      continue;
    }
    try {
      const result = tokenizeLine(line, lang, state);
      // The invariant, checked rather than trusted: if a scanner ever dropped
      // or duplicated a character, showing the wrong file contents would be a
      // far worse bug than showing them uncoloured.
      let length = 0;
      for (const token of result.tokens) length += token.text.length;
      if (length !== line.length) {
        out[i] = [{ text: line, kind: null }];
        state = initialState();
        continue;
      }
      out[i] = result.tokens;
      state = result.state;
    } catch {
      out[i] = [{ text: line, kind: null }];
      state = initialState();
    }
  }
  return out;
}
