/**
 * Tests for the file browser's syntax highlighter.
 *
 * Same mechanics as fileTree.test.mjs: Node's built-in runner, with the
 * TypeScript transpiled in-memory by the esbuild that Vite already pulls in.
 *
 *   cd frontend && npm test
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { transformSync } from 'esbuild';

const here = dirname(fileURLToPath(import.meta.url));
const dir = mkdtempSync(join(tmpdir(), 'asmgr-highlight-'));
const jsPath = join(dir, 'highlight.mjs');
writeFileSync(
  jsPath,
  transformSync(readFileSync(join(here, 'highlight.ts'), 'utf8'), { loader: 'ts', format: 'esm' }).code
);
const { detectLanguage, highlightLines, MAX_HIGHLIGHT_LINE_LENGTH } = await import(
  pathToFileURL(jsPath).href
);
process.on('exit', () => rmSync(dir, { recursive: true, force: true }));

/** Concatenate one line's tokens back into the text they came from. */
const join1 = (tokens) => tokens.map((t) => t.text).join('');

/** The kind assigned to the first token whose text is exactly `text`. */
const kindOf = (tokens, text) => (tokens.find((t) => t.text === text) || {}).kind;

// --- Language detection ---------------------------------------------------

test('detects languages by extension', () => {
  assert.equal(detectLanguage('app.go'), 'go');
  assert.equal(detectLanguage('src/lib/utils/fileTree.ts'), 'js');
  assert.equal(detectLanguage('main.tsx'), 'js');
  assert.equal(detectLanguage('script.py'), 'python');
  assert.equal(detectLanguage('lib.rs'), 'rust');
  assert.equal(detectLanguage('package.json'), 'json');
  assert.equal(detectLanguage('config.yaml'), 'yaml');
  assert.equal(detectLanguage('config.yml'), 'yaml');
  assert.equal(detectLanguage('README.md'), 'markdown');
  assert.equal(detectLanguage('deploy.sh'), 'shell');
  assert.equal(detectLanguage('schema.sql'), 'sql');
  assert.equal(detectLanguage('index.html'), 'html');
  assert.equal(detectLanguage('style.css'), 'css');
});

test('svelte files are highlighted as markup with embedded script and style', () => {
  assert.equal(detectLanguage('FileBrowser.svelte'), 'html');
});

test('detects languages by whole filename', () => {
  assert.equal(detectLanguage('go.mod'), 'go');
  assert.equal(detectLanguage('Dockerfile'), 'shell');
  assert.equal(detectLanguage('Makefile'), 'shell');
  assert.equal(detectLanguage('Cargo.toml'), 'toml');
  // The directory in front must not change the answer.
  assert.equal(detectLanguage('deep/nested/Dockerfile'), 'shell');
});

test('detection is case-insensitive', () => {
  assert.equal(detectLanguage('APP.GO'), 'go');
  assert.equal(detectLanguage('Readme.MD'), 'markdown');
});

test('unknown and extensionless files fall through to plain text', () => {
  assert.equal(detectLanguage('binary.wasm'), null);
  assert.equal(detectLanguage('LICENSE'), null);
  assert.equal(detectLanguage('notes'), null);
  assert.equal(detectLanguage('.env'), null); // a leading dot is a name, not an extension
  assert.equal(detectLanguage(''), null);
  assert.equal(detectLanguage('dir/'), null);
});

test('highlightLines returns null for an unknown language', () => {
  assert.equal(highlightLines(['anything at all'], null), null);
});

// --- Security: file content must never become live markup -----------------

test('HTML and script markup in a file is carried as inert text', () => {
  // The pane renders tokens through Svelte's text interpolation, never
  // {@html}, so the security boundary is that the tokens hold the ORIGINAL
  // characters and nothing else — no markup is ever synthesised from them.
  const hostile = [
    '<script>alert(1)</script>',
    '<img src=x onerror="alert(2)">',
    '"><svg/onload=alert(3)>',
    '&lt;already escaped&gt; & raw &amp;',
    '</code></div><script>alert(4)</script>',
  ];
  for (const lang of ['html', 'js', 'go', 'markdown', 'json', 'yaml', 'toml', 'css', 'shell']) {
    const out = highlightLines(hostile, lang);
    assert.equal(out.length, hostile.length, lang);
    out.forEach((tokens, i) => {
      // Round-trips exactly: nothing added, nothing escaped away, nothing lost.
      assert.equal(join1(tokens), hostile[i], `${lang} line ${i}`);
      for (const token of tokens) {
        assert.equal(typeof token.text, 'string');
        // A token carries text and a class name, never markup of its own.
        assert.ok(token.kind === null || typeof token.kind === 'string');
      }
    });
  }
});

test('a file made entirely of markup round-trips through the html scanner', () => {
  const lines = [
    '<!doctype html>',
    '<html><body onload="alert(1)">',
    '  <!-- <script>not really</script> -->',
    '  <script>const x = "</div>";</script>',
    '</body></html>',
  ];
  const out = highlightLines(lines, 'html');
  out.forEach((tokens, i) => assert.equal(join1(tokens), lines[i]));
});

// --- The core invariant: never alter the file's characters ----------------

/**
 * The one property that must hold for every language on every input: the
 * tokens concatenate back to the exact source line. Anything else means the
 * pane is showing something other than the file.
 */
function assertRoundTrip(lines, lang) {
  const out = highlightLines(lines, lang);
  assert.equal(out.length, lines.length);
  out.forEach((tokens, i) => {
    assert.equal(join1(tokens), lines[i], `${lang}: line ${i} was altered`);
  });
}

test('highlighting never alters the visible characters', () => {
  const samples = {
    go: [
      'package main',
      '',
      'import "fmt"',
      '// a comment with "quotes" and /* nested */ markers',
      '/* block',
      '   still inside',
      '   */ func main() {',
      '\tconst s = `raw \\n string` + "escaped \\" quote"',
      '\tx := 0x1f + 1_000 + 3.14e-2',
      '\tfmt.Println(s, x, true, nil)',
      '}',
    ],
    js: [
      "import { get } from 'svelte/store';",
      'export let active = false; // trailing',
      'const re = /not a string/g;',
      'const s = `template ${with} braces`;',
      'async function f<T>(x: T): Promise<T> { return x; }',
      'const o = { "key": 1, other: null };',
    ],
    python: [
      'def f(a, b=None):',
      '    """A docstring',
      '    spanning lines"""',
      "    return f'{a!r} and {b}'  # comment",
      '    x = 0b1010 + 1_000',
    ],
    rust: [
      'pub fn main() -> Result<(), Box<dyn Error>> {',
      '    let v: Vec<u8> = vec![1u8, 2, 3];',
      "    println!(\"{:?} {}\", v, 'c');",
      '    Ok(())',
      '}',
    ],
    json: ['{', '  "name": "asmgr",', '  "n": -1.5e3,', '  "ok": true, "x": null', '}'],
    yaml: [
      '# comment',
      '---',
      'key: value',
      'url: https://example.com/path#frag',
      'quoted: "a: b"  # trailing',
      'list:',
      '  - one',
      '  - two: 3',
      'empty:',
    ],
    toml: ['# c', '[package]', 'name = "x"', 'version = "0.1.0" # trailing', 'n = 42'],
    markdown: [
      '# Heading',
      '',
      'Text with `code`, **bold** and [a link](http://x).',
      '- a bullet',
      '1. numbered',
      '> quoted',
      '```go',
      'func main() {}',
      '```',
      'after the fence',
    ],
    shell: [
      '#!/usr/bin/env bash',
      'set -euo pipefail',
      'for f in *.go; do',
      '  echo "$f" | grep -q \'x\' && rm -- "$f"',
      'done',
    ],
    sql: [
      '-- comment',
      'SELECT a.id, COUNT(*) FROM t a',
      "WHERE a.name = 'x''y' AND a.n > 1.5",
      'GROUP BY a.id;',
    ],
    html: [
      '<div class="x" data-n=3 disabled>',
      '  text & entities &amp; more',
      '  <style>.a { color: #fff; }</style>',
      '</div>',
    ],
    css: [
      '.file-line code {',
      '  font-family: inherit; /* why */',
      '  color: rgba(255, 255, 255, 0.05);',
      '}',
      '@media (max-width: 100px) { .a { top: 0 } }',
    ],
  };
  for (const [lang, lines] of Object.entries(samples)) assertRoundTrip(lines, lang);
});

test('pathological input round-trips rather than throwing', () => {
  const nasty = [
    '',
    '   ',
    '\t\t',
    '"unterminated',
    "'",
    '`',
    '/*',
    '*/',
    '<',
    '>',
    '<<<<<<< HEAD',
    '\\',
    '\\\\',
    '0x',
    '1.2.3.4',
    '.5',
    'ünicode — ✓ 日本語',
    '🎉 emoji 🎉',
    '#',
    '--',
    'a:b:c:d',
    '{{{}}}',
    '=',
    '[',
  ];
  for (const lang of [
    'go', 'js', 'python', 'rust', 'json', 'yaml', 'toml', 'markdown', 'shell', 'sql', 'html', 'css',
  ]) {
    assertRoundTrip(nasty, lang);
  }
});

test('an unterminated block comment does not run away past its closer', () => {
  const lines = ['/* open', 'inside', '*/ code here', 'plain'];
  const out = highlightLines(lines, 'go');
  assert.equal(out[1][0].kind, 'comment');
  // The line that closes it must not leave the rest of the file commented out.
  assert.equal(out[3].every((t) => t.kind === 'comment'), false);
});

test('a stray quote does not colour the rest of the file as a string', () => {
  const lines = ["it's a plain sentence", 'const x = 1;'];
  const out = highlightLines(lines, 'js');
  assert.equal(out[1].some((t) => t.kind === 'keyword'), true);
});

// --- Colouring actually happens -------------------------------------------

test('marks the obvious constructs in each language', () => {
  const go = highlightLines(['func main() { return nil } // done'], 'go')[0];
  assert.equal(kindOf(go, 'func'), 'keyword');
  assert.equal(kindOf(go, 'nil'), 'literal');
  assert.equal(kindOf(go, '// done'), 'comment');

  const js = highlightLines(['const x = "hi"; // c'], 'js')[0];
  assert.equal(kindOf(js, 'const'), 'keyword');
  assert.equal(kindOf(js, '"hi"'), 'string');

  const json = highlightLines(['  "name": "asmgr",'], 'json')[0];
  assert.equal(kindOf(json, '"name"'), 'property');
  assert.equal(kindOf(json, '"asmgr"'), 'string');

  const yaml = highlightLines(['key: 42 # c'], 'yaml')[0];
  assert.equal(kindOf(yaml, 'key'), 'property');

  const md = highlightLines(['## Title'], 'markdown')[0];
  assert.equal(md[0].kind, 'heading');

  const toml = highlightLines(['[package]'], 'toml')[0];
  assert.equal(toml[0].kind, 'heading');

  const css = highlightLines(['  color: red;'], 'css')[0];
  assert.equal(kindOf(css, 'color'), 'property');

  const html = highlightLines(['<div class="a">'], 'html')[0];
  assert.equal(html.some((t) => t.kind === 'tag'), true);
  assert.equal(kindOf(html, 'class'), 'attr');
});

test('a svelte-style file colours its script body as code', () => {
  const lines = ['<script lang="ts">', '  export let active = false;', '</script>', '<div>x</div>'];
  const out = highlightLines(lines, 'html');
  out.forEach((tokens, i) => assert.equal(join1(tokens), lines[i]));
  assert.equal(kindOf(out[1], 'export'), 'keyword');
  // The markup after the script closes is markup again, not JavaScript.
  assert.equal(out[3].some((t) => t.kind === 'tag'), true);
});

// --- The large-input guards ------------------------------------------------

test('a very long line is left plain rather than tokenised', () => {
  const long = 'a'.repeat(MAX_HIGHLIGHT_LINE_LENGTH + 1);
  const out = highlightLines([long], 'js');
  assert.deepEqual(out[0], [{ text: long, kind: null }]);
});

test('an empty file and empty lines are handled', () => {
  assert.deepEqual(highlightLines([], 'go'), []);
  const out = highlightLines([''], 'go');
  assert.equal(join1(out[0]), '');
});

test('highlights a real source file of this repo without altering it', () => {
  // The regression that matters most: a genuine, long file must come back
  // character-identical. Uses this very test file, so it can't go stale.
  const source = readFileSync(join(here, 'highlight.ts'), 'utf8').split('\n');
  const out = highlightLines(source, 'js');
  assert.equal(out.length, source.length);
  out.forEach((tokens, i) => assert.equal(join1(tokens), source[i], `line ${i + 1}`));
});

// The C-family and scripting languages, added because a Kotlin file showed as
// plain text in the diff. They all resolve through the same table as the rest,
// so the file browser and the diff gain them together.
test('C-family extensions are recognised', () => {
  assert.equal(detectLanguage('Main.kt'), 'kotlin');
  assert.equal(detectLanguage('build.gradle.kts'), 'kotlin');
  assert.equal(detectLanguage('App.java'), 'java');
  assert.equal(detectLanguage('Program.cs'), 'csharp');
  assert.equal(detectLanguage('main.cpp'), 'cpp');
  assert.equal(detectLanguage('vector.hpp'), 'cpp');
  assert.equal(detectLanguage('main.c'), 'c');
  assert.equal(detectLanguage('stdio.h'), 'c');
  assert.equal(detectLanguage('Build.scala'), 'scala');
  assert.equal(detectLanguage('main.dart'), 'dart');
});

test('scripting languages are recognised', () => {
  assert.equal(detectLanguage('index.php'), 'php');
  assert.equal(detectLanguage('app.rb'), 'ruby');
  assert.equal(detectLanguage('init.lua'), 'lua');
  assert.equal(detectLanguage('View.swift'), 'swift');
});

test('a header is C, not C++', () => {
  // .h is ambiguous in practice, but the C mode reads C++ headers acceptably
  // while the reverse is worse: C++ keywords highlighted in C code are wrong
  // in a way a reader notices.
  assert.equal(detectLanguage('foo.h'), 'c');
  assert.equal(detectLanguage('foo.hpp'), 'cpp');
});

test('an unknown extension still has no language', () => {
  // The signal for "render this as plain text" — adding languages must not
  // turn that into a wrong guess.
  assert.equal(detectLanguage('notes.xyz'), null);
  assert.equal(detectLanguage('LICENSE'), null);
});
