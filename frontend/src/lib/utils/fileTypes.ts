/**
 * Maps a filename to a colour + glyph so the file lists can be scanned by type.
 *
 * One module rather than a table per view: the diff list and the file browser
 * must agree, and an extension known to one but not the other would show the
 * same file in two colours depending on which tab you were looking at.
 *
 * The colours are SEMANTIC and fixed. They sit next to the user's chosen accent
 * (uiThemes.ts), which can be any hue, so none of them may depend on the accent
 * to read correctly — they are all validated against the file-pane background
 * instead, and against that background tinted by every accent (the selected-row
 * state). See fileTypes.test.mjs for the measurements.
 *
 * Related languages deliberately share a colour. Sixty distinct hues would be
 * noise; a dozen families are learnable, and "this row is a stylesheet" is the
 * useful signal, not "this row is SCSS rather than LESS".
 */

/** The families. Grouping is by what a developer treats as interchangeable. */
export type FileTypeFamily =
  | 'go'
  | 'web'
  | 'script'
  | 'markup'
  | 'style'
  | 'systems'
  | 'jvm'
  | 'data'
  | 'doc'
  | 'shell'
  | 'media'
  | 'archive'
  | 'binary'
  | 'neutral';

export interface FileType {
  family: FileTypeFamily;
  /** Hex colour for the indicator. Never empty. */
  colour: string;
  /**
   * One or two characters drawn in the indicator. Deliberately NOT translated:
   * these are the language's own token (`go`, `TS`, `{}`), which developers read
   * the same way in every locale — and any translation would break the
   * two-character budget the narrow pane allows.
   */
  label: string;
}

/**
 * Contrast against the file-pane background, measured (see the test):
 *
 *   go       #5dd8e8  11.86:1     data     #7fd6a6  11.49:1
 *   web      #f0a860   9.97:1     doc      #a8b4c4   9.51:1
 *   script   #f2d16b  13.43:1     shell    #8fd68f  11.60:1
 *   markup   #f08a6c   8.15:1     media    #e89ac8   9.41:1
 *   style    #9db4f0   9.73:1     archive  #c0a888   8.76:1
 *   systems  #e88a8a   8.03:1     binary   #7d8590   5.36:1
 *   jvm      #c9a0dc   9.09:1     neutral  #6b7280   4.14:1
 *
 * The worst case is the selected row under the brightest accent (Amber), which
 * lifts the background by at most 1.17:1 — every colour above still clears 3.5:1
 * there, so nothing disappears into the selection tint.
 *
 * `neutral` is intentionally the same #6b7280 the app already uses for secondary
 * text: an unrecognised file should read as "no information", not as a family.
 */
const FAMILY_COLOURS: Record<FileTypeFamily, string> = {
  go: '#5dd8e8',
  web: '#f0a860',
  script: '#f2d16b',
  markup: '#f08a6c',
  style: '#9db4f0',
  systems: '#e88a8a',
  jvm: '#c9a0dc',
  data: '#7fd6a6',
  doc: '#a8b4c4',
  shell: '#8fd68f',
  media: '#e89ac8',
  archive: '#c0a888',
  binary: '#7d8590',
  neutral: '#6b7280',
};

/** A family plus the glyph, before the colour is filled in. */
type Entry = [FileTypeFamily, string];

/**
 * Exact filenames, checked FIRST. `Dockerfile` and `Makefile` have no extension
 * at all, and `go.mod` would otherwise be read as a `.mod` file — the whole name
 * is the type here.
 */
const BY_NAME: Record<string, Entry> = {
  'dockerfile': ['data', 'dkr'],
  'containerfile': ['data', 'dkr'],
  'docker-compose.yml': ['data', 'dkr'],
  'docker-compose.yaml': ['data', 'dkr'],
  'makefile': ['shell', 'mk'],
  'gnumakefile': ['shell', 'mk'],
  'cmakelists.txt': ['shell', 'mk'],
  'justfile': ['shell', 'mk'],
  'rakefile': ['shell', 'mk'],
  'go.mod': ['go', 'go'],
  'go.sum': ['go', 'go'],
  'go.work': ['go', 'go'],
  'package.json': ['data', 'np'],
  'package-lock.json': ['binary', 'lck'],
  'yarn.lock': ['binary', 'lck'],
  'pnpm-lock.yaml': ['binary', 'lck'],
  'bun.lockb': ['binary', 'lck'],
  'composer.lock': ['binary', 'lck'],
  'gemfile': ['script', 'rb'],
  'gemfile.lock': ['binary', 'lck'],
  'cargo.toml': ['systems', 'rs'],
  'cargo.lock': ['binary', 'lck'],
  'poetry.lock': ['binary', 'lck'],
  'pipfile': ['data', 'py'],
  'pipfile.lock': ['binary', 'lck'],
  'requirements.txt': ['data', 'py'],
  'pyproject.toml': ['data', 'py'],
  'tsconfig.json': ['data', 'cfg'],
  'jsconfig.json': ['data', 'cfg'],
  '.gitignore': ['doc', 'git'],
  '.gitattributes': ['doc', 'git'],
  '.gitmodules': ['doc', 'git'],
  '.dockerignore': ['doc', 'git'],
  '.editorconfig': ['doc', 'cfg'],
  '.env': ['data', 'env'],
  '.npmrc': ['data', 'cfg'],
  '.nvmrc': ['data', 'cfg'],
  '.prettierrc': ['data', 'cfg'],
  '.eslintrc': ['data', 'cfg'],
  'license': ['doc', 'lic'],
  'license.md': ['doc', 'lic'],
  'licence': ['doc', 'lic'],
  'readme': ['doc', 'md'],
  'readme.md': ['doc', 'md'],
  'changelog.md': ['doc', 'md'],
  'claude.md': ['doc', 'md'],
};

/**
 * Compound suffixes, checked BEFORE the plain extension. `foo.test.ts` and
 * `foo.d.ts` are different things to work on than `foo.ts`, and `.tar.gz` is one
 * archive rather than a gzip of something unknown.
 *
 * Order matters within this list only in that longer suffixes must be tried
 * first; the lookup below walks it in order, so keep the specific ones on top.
 */
const BY_COMPOUND: [string, Entry][] = [
  ['.d.ts', ['web', 'd.ts']],
  ['.test.ts', ['web', 'test']],
  ['.test.tsx', ['web', 'test']],
  ['.test.js', ['script', 'test']],
  ['.test.jsx', ['script', 'test']],
  ['.test.mjs', ['script', 'test']],
  ['.spec.ts', ['web', 'test']],
  ['.spec.js', ['script', 'test']],
  ['.test.go', ['go', 'test']],
  ['.test.py', ['script', 'test']],
  ['.tar.gz', ['archive', 'zip']],
  ['.tar.bz2', ['archive', 'zip']],
  ['.tar.xz', ['archive', 'zip']],
  ['.tar.zst', ['archive', 'zip']],
];

/** Plain extensions, without the leading dot. */
const BY_EXTENSION: Record<string, Entry> = {
  // Go — its own colour because this project and its users live in it.
  go: ['go', 'go'],
  mod: ['go', 'go'],
  sum: ['go', 'go'],

  // TypeScript and the component frameworks that compile to the same app.
  ts: ['web', 'TS'],
  tsx: ['web', 'TSX'],
  svelte: ['web', 'sv'],
  vue: ['web', 'vue'],
  astro: ['web', 'as'],

  // JavaScript proper, kept distinct from TypeScript — the difference is the
  // one a developer scanning a mixed repo actually cares about.
  js: ['script', 'JS'],
  jsx: ['script', 'JSX'],
  mjs: ['script', 'JS'],
  cjs: ['script', 'JS'],

  // Dynamic languages share `script`; they occupy the same slot in a project.
  py: ['script', 'py'],
  pyi: ['script', 'py'],
  rb: ['script', 'rb'],
  php: ['script', 'php'],
  lua: ['script', 'lua'],
  pl: ['script', 'pl'],
  r: ['script', 'R'],

  // Markup and templates.
  html: ['markup', '<>'],
  htm: ['markup', '<>'],
  xml: ['markup', '<>'],
  svg: ['markup', 'svg'],
  hbs: ['markup', '<>'],
  ejs: ['markup', '<>'],
  twig: ['markup', '<>'],
  erb: ['markup', '<>'],
  templ: ['markup', '<>'],

  // Stylesheets.
  css: ['style', 'css'],
  scss: ['style', 'sc'],
  sass: ['style', 'sc'],
  less: ['style', 'le'],
  styl: ['style', 'st'],
  postcss: ['style', 'css'],

  // Systems languages.
  rs: ['systems', 'rs'],
  c: ['systems', 'c'],
  h: ['systems', 'h'],
  cc: ['systems', 'c++'],
  cpp: ['systems', 'c++'],
  cxx: ['systems', 'c++'],
  hpp: ['systems', 'h++'],
  hh: ['systems', 'h++'],
  zig: ['systems', 'zig'],
  nim: ['systems', 'nim'],

  // JVM and .NET — managed runtimes, one family.
  java: ['jvm', 'jv'],
  kt: ['jvm', 'kt'],
  kts: ['jvm', 'kt'],
  scala: ['jvm', 'sc'],
  groovy: ['jvm', 'gr'],
  gradle: ['jvm', 'gr'],
  cs: ['jvm', 'C#'],
  fs: ['jvm', 'F#'],
  vb: ['jvm', 'vb'],
  swift: ['jvm', 'sw'],
  dart: ['jvm', 'dt'],

  // Structured data and configuration.
  json: ['data', '{}'],
  jsonc: ['data', '{}'],
  json5: ['data', '{}'],
  yaml: ['data', 'yml'],
  yml: ['data', 'yml'],
  toml: ['data', 'tml'],
  ini: ['data', 'ini'],
  conf: ['data', 'cfg'],
  cfg: ['data', 'cfg'],
  properties: ['data', 'cfg'],
  env: ['data', 'env'],
  csv: ['data', 'csv'],
  tsv: ['data', 'tsv'],
  proto: ['data', 'pb'],
  graphql: ['data', 'gql'],
  gql: ['data', 'gql'],

  // SQL sits with data: it describes and moves rows, not program flow.
  sql: ['data', 'sql'],

  // Prose and documentation.
  md: ['doc', 'md'],
  mdx: ['doc', 'md'],
  markdown: ['doc', 'md'],
  rst: ['doc', 'rst'],
  txt: ['doc', 'txt'],
  adoc: ['doc', 'ad'],
  tex: ['doc', 'tex'],
  pdf: ['doc', 'pdf'],

  // Shells and anything else that is "run this".
  sh: ['shell', '$'],
  bash: ['shell', '$'],
  zsh: ['shell', '$'],
  fish: ['shell', '$'],
  ps1: ['shell', '$'],
  bat: ['shell', '$'],
  cmd: ['shell', '$'],
  mk: ['shell', 'mk'],

  // Images, fonts, audio, video — anything shown rather than read.
  png: ['media', 'img'],
  jpg: ['media', 'img'],
  jpeg: ['media', 'img'],
  gif: ['media', 'img'],
  webp: ['media', 'img'],
  avif: ['media', 'img'],
  bmp: ['media', 'img'],
  ico: ['media', 'ico'],
  woff: ['media', 'fnt'],
  woff2: ['media', 'fnt'],
  ttf: ['media', 'fnt'],
  otf: ['media', 'fnt'],
  mp3: ['media', 'aud'],
  wav: ['media', 'aud'],
  ogg: ['media', 'aud'],
  mp4: ['media', 'vid'],
  webm: ['media', 'vid'],
  mov: ['media', 'vid'],

  // Archives.
  zip: ['archive', 'zip'],
  gz: ['archive', 'gz'],
  bz2: ['archive', 'bz2'],
  xz: ['archive', 'xz'],
  zst: ['archive', 'zst'],
  tar: ['archive', 'tar'],
  rar: ['archive', 'rar'],
  '7z': ['archive', '7z'],
  jar: ['archive', 'jar'],
  war: ['archive', 'war'],

  // Build output and other things not meant to be opened.
  exe: ['binary', 'bin'],
  dll: ['binary', 'bin'],
  so: ['binary', 'bin'],
  dylib: ['binary', 'bin'],
  o: ['binary', 'obj'],
  a: ['binary', 'lib'],
  wasm: ['binary', 'wasm'],
  class: ['binary', 'cls'],
  pyc: ['binary', 'pyc'],
  lock: ['binary', 'lck'],
  db: ['binary', 'db'],
  sqlite: ['binary', 'db'],
  sqlite3: ['binary', 'db'],
  bin: ['binary', 'bin'],
};

/** What an unrecognised — or extensionless — file gets. Never blank. */
const UNKNOWN: FileType = { family: 'neutral', colour: FAMILY_COLOURS.neutral, label: '·' };

/**
 * Classify a file by its name.
 *
 * Accepts a bare basename or a full path; only the last segment is examined, so
 * callers don't have to split first. Matching is case-insensitive because
 * `README.MD`, `Makefile` and `.GitIgnore` all turn up in real repositories.
 */
export function fileTypeOf(pathOrName: string): FileType {
  const name = basename(pathOrName).toLowerCase();
  if (!name) return UNKNOWN;

  const exact = BY_NAME[name];
  if (exact) return build(exact);

  for (const [suffix, entry] of BY_COMPOUND) {
    if (name.endsWith(suffix)) return build(entry);
  }

  const ext = extensionOf(name);
  if (ext) {
    const byExt = BY_EXTENSION[ext];
    if (byExt) return build(byExt);
  }

  return UNKNOWN;
}

function build(entry: Entry): FileType {
  return { family: entry[0], colour: FAMILY_COLOURS[entry[0]], label: entry[1] };
}

function basename(path: string): string {
  const idx = path.lastIndexOf('/');
  return idx < 0 ? path : path.slice(idx + 1);
}

/**
 * The extension, or '' when there is none.
 *
 * A leading dot does NOT start an extension: `.gitignore` is a whole filename,
 * not a file of type "gitignore". That is why the exact-name table above carries
 * the dotfiles, and why anything else beginning with a dot falls through to the
 * neutral default rather than matching some unrelated extension.
 */
function extensionOf(name: string): string {
  const idx = name.lastIndexOf('.');
  if (idx <= 0) return '';
  return name.slice(idx + 1);
}
