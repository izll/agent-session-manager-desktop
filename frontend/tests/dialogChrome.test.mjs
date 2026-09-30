import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

// Every dialog wears the same chrome — the overlay, the panel, the header with
// its title and ✕, the body's padding, the footer — and it is the one in
// style.css, which the Settings dialog shows. The background-agents, servers
// and saved-commands dialogs had each restyled it locally: a 15–16px title
// instead of 18, a bare "×" glyph instead of the 20px icon, a borderless close
// button, 14px/18px padding instead of 20/24, a 12px corner instead of 16. Each
// copy looked reasonable on its own; side by side they were visibly different
// windows. These tests keep the chrome in one place.

const src = fileURLToPath(new URL('../src/', import.meta.url));

function svelteFiles(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) out.push(...svelteFiles(path));
    else if (name.endsWith('.svelte')) out.push(path);
  }
  return out;
}

const files = svelteFiles(src).map((path) => {
  const text = readFileSync(path, 'utf8');
  const at = text.indexOf('<style');
  return {
    name: relative(src, path),
    markup: at < 0 ? text : text.slice(0, at),
    css: at < 0 ? '' : text.slice(at)
      .replace(/^<style[^>]*>/, '').replace(/<\/style>[\s\S]*$/, '')
      .replace(/\/\*[\s\S]*?\*\//g, ''),
  };
});

const withDialogs = files.filter((f) => f.markup.includes('class="dialog-content'));

test('there are dialogs to check', () => {
  assert.ok(withDialogs.length > 30, `only found ${withDialogs.length} files with dialogs`);
  // Named, so a dialog that drops the shared panel class drops out loudly
  // rather than quietly escaping every check below.
  for (const name of ['Dialogs/SettingsDialog.svelte', 'Dialogs/WhatsNewDialog.svelte', 'Dialogs/ProjectTasksDialog.svelte']) {
    const file = withDialogs.find((f) => f.name.endsWith(name));
    assert.ok(file, `${name} is not checked`);
    assert.match(file.markup, /<div class="dialog-header">/, `${name} has no shared header`);
  }
});

// ── The close button ──────────────────────────────────────────────────────

test('every dialog header closes with the shared button', () => {
  const wrong = [];
  for (const f of files) {
    if (f.name.endsWith('DialogCloseButton.svelte')) continue;
    // Its own markup drifted before — a "×" at the dialog's font size, an svg
    // at 16px — so no dialog writes it by hand any more.
    if (/class="close-btn[\s"]/.test(f.markup)) wrong.push(`${f.name}: hand-written close button`);
    const headers = (f.markup.match(/class="dialog-header"/g) || []).length;
    const buttons = (f.markup.match(/<DialogCloseButton\b/g) || []).length;
    if (buttons < headers) wrong.push(`${f.name}: ${headers} header(s), ${buttons} DialogCloseButton(s)`);
  }
  assert.deepEqual(wrong, []);
});

test('the shared close button is the 20px icon, labelled', () => {
  const button = readFileSync(new URL('../src/lib/components/common/DialogCloseButton.svelte', import.meta.url), 'utf8');
  assert.match(button, /class="close-btn"/);
  assert.match(button, /<svg width="20" height="20"/, 'the ✕ is 20px, as in Settings');
  assert.match(button, /aria-label=\{label \?\? \$t\('common\.close'\)\}/, 'an icon-only button needs a name');
});

test('a dialog title is an h2, so it takes the shared title style', () => {
  const wrong = [];
  for (const f of files) {
    for (const m of f.markup.matchAll(/class="dialog-header"[^>]*>([\s\S]*?)<\/h[1-6]>/g)) {
      const heading = m[1].match(/<h([1-6])/);
      if (!heading || heading[1] !== '2') wrong.push(`${f.name}: <h${heading?.[1]}> in a dialog header`);
    }
  }
  assert.deepEqual(wrong, []);
});

// ── One header layout ─────────────────────────────────────────────────────

// Every dialog header's markup, from its opening tag to the matching </div>.
function headers(markup) {
  const out = [];
  for (const m of markup.matchAll(/<div class="dialog-header">/g)) {
    let depth = 0;
    const tags = /<div\b|<\/div>/g;
    tags.lastIndex = m.index;
    for (let t; (t = tags.exec(markup));) {
      depth += t[0] === '</div>' ? -1 : 1;
      if (depth === 0) { out.push(markup.slice(m.index, tags.lastIndex)); break; }
    }
  }
  return out;
}

const headerFiles = files.map((f) => ({ ...f, headers: headers(f.markup) })).filter((f) => f.headers.length);

test('there are dialog headers to check', () => {
  const count = headerFiles.reduce((n, f) => n + f.headers.length, 0);
  assert.ok(count > 35, `only found ${count} dialog headers`);
});

// The history dialog's title had a class of its own that made it truncate;
// the others wrapped, and a task title three lines long squeezed the ✕ to
// 20px. The truncation is the shared title's now, and a class on it is where
// a local variant would start again.
test('a dialog title is plain: no class, nothing but its text', () => {
  const wrong = [];
  for (const f of headerFiles) {
    for (const h of f.headers) {
      for (const m of h.matchAll(/<h2([^>]*)>([\s\S]*?)<\/h2>/g)) {
        if (/\bclass[=:]/.test(m[1])) wrong.push(`${f.name}: a class on the title`);
        // A count badge inside the h2 took the title's clipped gradient.
        if (/<[a-zA-Z]/.test(m[2])) wrong.push(`${f.name}: markup inside the title — ${m[2].trim().slice(0, 60)}`);
      }
    }
  }
  assert.deepEqual(wrong, []);
});

// Extra buttons were a 30px box (history), a bare 14px glyph (the task
// dialogs' microphone) and an underlined link (new session). Now each is a
// .dialog-header-btn, grouped with the ✕ at the right end, the ✕ last.
test('extra header buttons are the shared kind, before the ✕', () => {
  const wrong = [];
  for (const f of headerFiles) {
    for (const h of f.headers) {
      const buttons = [...h.matchAll(/<button\b[^>]*>/g)].map((m) => m[0]);
      for (const b of buttons) {
        if (!/class="dialog-header-btn[\s"]/.test(b)) wrong.push(`${f.name}: ${b.slice(0, 70)}`);
      }
      if (buttons.length && !/<div class="dialog-header-actions">/.test(h)) {
        wrong.push(`${f.name}: extra buttons outside .dialog-header-actions`);
      }
      const close = h.lastIndexOf('<DialogCloseButton');
      const lastButton = h.lastIndexOf('<button');
      if (close < lastButton) wrong.push(`${f.name}: the ✕ is not the last button`);
    }
  }
  assert.deepEqual(wrong, []);
});

// ── No local copies of the chrome ────────────────────────────────────────

// Rules in a component's own <style>, as [selector, body] pairs.
function rules(css) {
  const out = [];
  for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    for (const sel of m[1].split(',')) out.push([sel.trim(), m[2]]);
  }
  return out;
}

const props = (body) => body.split(';').map((d) => d.split(':')[0].trim()).filter(Boolean);

// Deliberate differences, each with its reason. Anything not listed here
// uses the shared look.
const allowed = {
  // A search launcher: it sits high on the screen and can go full-screen,
  // with a results/preview split that runs edge to edge.
  'lib/components/Dialogs/GlobalSearchDialog.svelte': [
    '.dialog-overlay padding', '.dialog-overlay.fullscreen padding',
    '.dialog-content.fullscreen border-radius',
    '.dialog-body padding', '.dialog-footer padding',
  ],
  // Hint strips, not button rows: one line of small text under a list.
  'lib/components/Dialogs/SettingsDialog.svelte': ['.dialog-footer padding', '.dialog-footer border-top'],
  'lib/components/Dialogs/QuickJumpDialog.svelte': ['.dialog-footer padding', '.dialog-footer border-top'],
  // Confirmations: no header, a centred icon, title and buttons — the panel
  // pads itself because there is no header or body to do it.
  'lib/components/Dialogs/ConfirmDialog.svelte': ['.dialog-content padding'],
  'lib/components/Dialogs/StartDialog.svelte': ['.dialog-content padding'],
  'lib/components/Dialogs/StopDialog.svelte': ['.dialog-content padding'],
  'lib/components/Dialogs/ResumeChoiceDialog.svelte': ['.dialog-content padding'],
  // A one-field prompt with no header, sized like the confirmations.
  'lib/components/Dialogs/QuickTerminalDialog.svelte': ['.quick-terminal padding'],
};

// What each piece of chrome may not redefine.
const chrome = {
  '.dialog-overlay': ['background', 'backdrop-filter', 'padding', 'z-index'],
  '.dialog-content': ['background', 'border', 'border-radius', 'box-shadow', 'padding'],
  '.dialog-header': null, // null: no local rule at all
  '.close-btn': null,
  '.dialog-header-actions': null,
  '.dialog-header-btn': ['width', 'height', 'padding', 'border', 'border-radius', 'background', 'font-size', 'margin', 'margin-left', 'margin-right'],
  '.dialog-heading': null,
  '.dialog-subtitle': null,
  '.dialog-count': null,
  '.dialog-body': ['padding', 'background'],
  '.dialog-footer': ['padding', 'background', 'border', 'border-top'],
  '.dialog-actions': ['padding', 'padding-top', 'background', 'border', 'border-top'],
};

test('no component restyles the dialog chrome', () => {
  const found = [];
  for (const f of files) {
    if (!f.css) continue;
    const ok = new Set(allowed[f.name] || []);
    // A dialog's own class on its panel (class="dialog-content manager") is
    // the panel too: the same limits apply to it.
    const panelClasses = [...f.markup.matchAll(/class="dialog-content ([^"]+)"/g)]
      .flatMap((m) => m[1].split(/\s+/)).map((c) => '.' + c);

    for (const [selector, body] of rules(f.css)) {
      // The piece of chrome the selector ends on, if any.
      const last = selector.split(/[\s>+~]+/).pop() || '';
      let piece = Object.keys(chrome).find((c) => last === c || last.startsWith(c + '.') || last.startsWith(c + ':'));
      if (/\.dialog-header\s+h2\b/.test(selector)) {
        found.push(`${f.name}: ${selector} — the title style is shared`);
        continue;
      }
      // Anything else inside a header that is not one of its shared parts is
      // the dialog's own content, and not chrome. (The recovery center's
      // subtitle was styled like this before it became .dialog-subtitle.)
      if (!piece && /\.dialog-header\s+\S/.test(selector)) {
        found.push(`${f.name}: ${selector} — a header part styled locally; use the shared ones`);
        continue;
      }
      let key = piece;
      if (!piece) {
        const own = panelClasses.find((c) => last === c);
        if (!own) continue;
        piece = '.dialog-content';
        key = own;
      }
      if (last.includes(':hover') && piece !== '.close-btn') continue;
      const banned = chrome[piece];
      if (banned === null) {
        found.push(`${f.name}: ${selector} — the ${piece} is shared, not restyled`);
        continue;
      }
      const base = last.replace(/:.*$/, '');
      for (const p of props(body)) {
        if (!banned.includes(p)) continue;
        if (ok.has(`${base} ${p}`) || ok.has(`${key} ${p}`)) continue;
        found.push(`${f.name}: ${selector} sets ${p}`);
      }
    }
  }
  assert.deepEqual(found, [], 'dialogs restyling the shared chrome:\n' + found.join('\n'));
});

// The shared close button has to cover what the local copies did: a
// disabled state (import and update lock it while they run).
test('the shared close button has a disabled state', () => {
  const sheet = readFileSync(new URL('../src/style.css', import.meta.url), 'utf8');
  assert.match(sheet, /\.close-btn:disabled\s*\{[^}]*opacity/);
  assert.match(sheet, /\.close-btn:hover:not\(:disabled\)/);
});
