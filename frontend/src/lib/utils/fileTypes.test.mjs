/**
 * Tests for the file-type classifier, plus the contrast measurements that
 * justify the palette.
 *
 * Same setup as fileTree.test.mjs: Node's built-in runner, with the esbuild that
 * Vite already pulls in transpiling the TypeScript — no new dependency.
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
const dir = mkdtempSync(join(tmpdir(), 'asmgr-filetypes-'));
const jsPath = join(dir, 'fileTypes.mjs');
writeFileSync(
  jsPath,
  transformSync(readFileSync(join(here, 'fileTypes.ts'), 'utf8'), { loader: 'ts', format: 'esm' }).code
);
const { fileTypeOf } = await import(pathToFileURL(jsPath).href);
process.on('exit', () => rmSync(dir, { recursive: true, force: true }));

test('an exact filename wins over its extension', () => {
  // go.mod would otherwise be read as a ".mod" file, and package.json as plain
  // JSON — the whole name carries the meaning here.
  assert.equal(fileTypeOf('go.mod').family, 'go');
  assert.equal(fileTypeOf('go.sum').family, 'go');
  assert.equal(fileTypeOf('package.json').label, 'np');
  assert.notEqual(fileTypeOf('package.json').label, fileTypeOf('data.json').label);
  // Cargo.toml is Rust, not generic TOML.
  assert.equal(fileTypeOf('Cargo.toml').family, 'systems');
  assert.equal(fileTypeOf('config.toml').family, 'data');
});

test('extensionless names are recognised by name alone', () => {
  assert.equal(fileTypeOf('Dockerfile').family, 'data');
  assert.equal(fileTypeOf('Makefile').family, 'shell');
  assert.equal(fileTypeOf('Gemfile').family, 'script');
});

test('compound extensions beat the plain extension', () => {
  assert.equal(fileTypeOf('app.d.ts').label, 'd.ts');
  assert.equal(fileTypeOf('app.ts').label, 'TS');
  assert.equal(fileTypeOf('store.test.ts').label, 'test');
  assert.equal(fileTypeOf('store.spec.js').label, 'test');
  assert.equal(fileTypeOf('handler.test.go').label, 'test');
  // A tarball is one archive, not a gzip of something unidentified.
  assert.equal(fileTypeOf('release.tar.gz').family, 'archive');
  assert.equal(fileTypeOf('release.tar.gz').label, 'zip');
  assert.equal(fileTypeOf('dump.gz').label, 'gz');
});

test('matching ignores case', () => {
  assert.deepEqual(fileTypeOf('README.MD'), fileTypeOf('readme.md'));
  assert.deepEqual(fileTypeOf('DOCKERFILE'), fileTypeOf('Dockerfile'));
  assert.deepEqual(fileTypeOf('Main.GO'), fileTypeOf('main.go'));
  assert.deepEqual(fileTypeOf('App.Test.Ts'), fileTypeOf('app.test.ts'));
});

test('an unknown extension falls back to the neutral default', () => {
  const t = fileTypeOf('mystery.qqzz');
  assert.equal(t.family, 'neutral');
  // Never blank — the row must always render something.
  assert.ok(t.colour);
  assert.ok(t.label);
});

test('a file with no extension at all falls back too', () => {
  const t = fileTypeOf('CONTRIBUTING');
  assert.equal(t.family, 'neutral');
  assert.ok(t.colour);
  assert.ok(t.label);
  assert.deepEqual(fileTypeOf('notes'), t);
});

test('a dotfile is a whole name, not an extension', () => {
  // ".gitignore" must not be read as a file of type "gitignore".
  assert.equal(fileTypeOf('.gitignore').family, 'doc');
  assert.equal(fileTypeOf('.gitignore').label, 'git');
  assert.equal(fileTypeOf('.editorconfig').family, 'doc');
  // An unlisted dotfile has no extension to fall back on, so it goes neutral
  // rather than matching something unrelated.
  assert.equal(fileTypeOf('.mysteryrc').family, 'neutral');
});

test('a dotfile that also carries a real extension still uses it', () => {
  assert.equal(fileTypeOf('.eslintrc.json').label, '{}');
  assert.equal(fileTypeOf('.babelrc.js').label, 'JS');
});

test('only the last path segment is examined', () => {
  assert.deepEqual(fileTypeOf('src/lib/utils/fileTypes.ts'), fileTypeOf('fileTypes.ts'));
  // A directory named like an extension must not leak into the result.
  assert.equal(fileTypeOf('go/README.md').family, 'doc');
});

test('degenerate input does not throw', () => {
  for (const input of ['', '.', '..', '/', 'a/', '...ts']) {
    const t = fileTypeOf(input);
    assert.ok(t.colour, `no colour for ${JSON.stringify(input)}`);
    assert.ok(t.label, `no label for ${JSON.stringify(input)}`);
  }
});

test('labels stay short enough for the narrow pane', () => {
  const names = [
    'main.go', 'app.ts', 'App.svelte', 'index.js', 'main.py', 'style.scss',
    'lib.rs', 'Main.java', 'data.json', 'README.md', 'run.sh', 'logo.png',
    'src.tar.gz', 'a.out.wasm', 'go.mod', 'Dockerfile', 'yarn.lock', 'x.d.ts',
  ];
  for (const n of names) {
    const t = fileTypeOf(n);
    assert.ok(t.label.length <= 4, `${n} -> "${t.label}" is too long`);
  }
});

// --- Contrast ---------------------------------------------------------------

/** Relative luminance, per WCAG — the same formula uiThemes.ts uses. */
function luminance([r, g, b]) {
  const c = [r, g, b].map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}

function hexToRgb(hex) {
  const v = hex.replace('#', '');
  return [parseInt(v.slice(0, 2), 16), parseInt(v.slice(2, 4), 16), parseInt(v.slice(4, 6), 16)];
}

function contrast(a, b) {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** Composite `fg` at alpha `a` over `bg`. */
function over(fg, a, bg) {
  return fg.map((v, i) => Math.round(v * a + bg[i] * (1 - a)));
}

// The file pane is #0a0a0f under a rgba(0,0,0,0.2) overlay.
const PANE_BG = over([0, 0, 0], 0.2, [10, 10, 15]);

// Every accent the user can pick (uiThemes.ts), tinting the selected row at
// 0.12 — the state where a type colour is most at risk of washing out.
const ACCENTS = [
  [139, 92, 246], [59, 130, 246], [20, 184, 166], [34, 197, 94],
  [245, 158, 11], [244, 63, 94], [6, 182, 212], [100, 116, 139],
];

/** One representative filename per family, so every colour gets measured. */
const SAMPLES = [
  'main.go', 'app.ts', 'index.js', 'page.html', 'style.css', 'lib.rs',
  'Main.java', 'data.json', 'README.md', 'run.sh', 'logo.png', 'src.zip',
  'a.wasm', 'mystery.qqzz',
];

test('every family colour is legible on the file-pane background', () => {
  const seen = new Set();
  for (const name of SAMPLES) {
    const { family, colour } = fileTypeOf(name);
    seen.add(family);
    const ratio = contrast(hexToRgb(colour), PANE_BG);
    // 3:1 is WCAG AA for non-text/large-text; these are small glyphs so they
    // are all held well above it.
    assert.ok(ratio >= 4, `${family} ${colour} is only ${ratio.toFixed(2)}:1 on the pane`);
  }
  assert.equal(seen.size, SAMPLES.length, 'each sample should hit a distinct family');
});

test('no accent makes a family colour disappear in the selected row', () => {
  for (const name of SAMPLES) {
    const { family, colour } = fileTypeOf(name);
    const fg = hexToRgb(colour);
    for (const accent of ACCENTS) {
      const ratio = contrast(fg, over(accent, 0.12, PANE_BG));
      assert.ok(
        ratio >= 3,
        `${family} ${colour} drops to ${ratio.toFixed(2)}:1 on the selected row`
      );
    }
  }
});

test('distinct families do not share a colour', () => {
  const byColour = new Map();
  for (const name of SAMPLES) {
    const { family, colour } = fileTypeOf(name);
    const prior = byColour.get(colour);
    assert.ok(!prior || prior === family, `${family} and ${prior} share ${colour}`);
    byColour.set(colour, family);
  }
});
