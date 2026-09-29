import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The release-notes dialog: the changelog's inline markdown, the pages the
// releases are arranged into, and the queue that keeps it from opening on top
// of another launch-time dialog.

const notes = await import('../src/lib/utils/whatsNew.ts');
const { createLaunchDialogQueue } = await import('../src/lib/utils/launchDialogs.ts');
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

// ── Inline markdown ────────────────────────────────────────────────────────

test('bold lead-ins, code and links become segments', () => {
  assert.deepEqual(notes.parseInline('**Find works.** Press `Ctrl+F` or see [#1](https://github.com/x/y/pull/1).'), [
    { type: 'strong', children: [{ type: 'text', text: 'Find works.' }] },
    { type: 'text', text: ' Press ' },
    { type: 'code', text: 'Ctrl+F' },
    { type: 'text', text: ' or see ' },
    { type: 'link', href: 'https://github.com/x/y/pull/1', children: [{ type: 'text', text: '#1' }] },
    { type: 'text', text: '.' },
  ]);
});

test('bold may hold code, and code may hold asterisks', () => {
  assert.deepEqual(notes.parseInline('**The `--flag` works**'), [
    { type: 'strong', children: [
      { type: 'text', text: 'The ' }, { type: 'code', text: '--flag' }, { type: 'text', text: ' works' },
    ] },
  ]);
  assert.deepEqual(notes.parseInline('**a `**` b**'), [
    { type: 'strong', children: [{ type: 'text', text: 'a ' }, { type: 'code', text: '**' }, { type: 'text', text: ' b' }] },
  ]);
});

test('unmatched markers stay as they are', () => {
  assert.deepEqual(notes.parseInline('2 ** 3 and a lone ` tick'), [{ type: 'text', text: '2 ** 3 and a lone ` tick' }]);
  assert.deepEqual(notes.parseInline('\\*\\*not bold\\*\\*'), [{ type: 'text', text: '**not bold**' }]);
});

test('markup in the notes is text, and only web links link', () => {
  const html = '<img src=x onerror=alert(1)><script>alert(1)</script>';
  assert.deepEqual(notes.parseInline(html), [{ type: 'text', text: html }]);
  assert.deepEqual(notes.parseInline('[click](javascript:alert(1))'), [{ type: 'text', text: '[click](javascript:alert(1))' }]);
  assert.deepEqual(notes.parseInline('[file](file:///etc/passwd)'), [{ type: 'text', text: 'file' }]);
  assert.equal(notes.safeHref('JavaScript:alert(1)'), null);
  assert.equal(notes.safeHref('https://example.com'), 'https://example.com');
});

test('the notes are drawn as text, never as HTML', () => {
  for (const file of ['../src/lib/components/common/InlineMarkdown.svelte', '../src/lib/components/Dialogs/WhatsNewDialog.svelte']) {
    assert.doesNotMatch(read(file), /\{@html/, `${file} injects HTML`);
  }
});

// ── Pages ─────────────────────────────────────────────────────────────────

const entry = (version) => ({ version, date: '2026-09-01', intro: [], sections: [] });
const changelog = ['1.1.18', '1.1.17', '1.1.16', '1.1.15', '1.1.14', '1.1.13'].map(entry);
const keys = (pages) => pages.map((p) => p.key);

test('opened by hand: one release per page, starting at the running one', () => {
  const pages = notes.buildPages(changelog);
  assert.deepEqual(keys(pages), ['1.1.18', '1.1.17', '1.1.16', '1.1.15', '1.1.14', '1.1.13']);
  assert.equal(notes.startPage(pages, '1.1.16'), 2);
  assert.equal(notes.startPage(pages, '9.9.9'), 0, 'an unknown version opens on the newest');
});

test('after an update over several releases, they share the first page', () => {
  const pages = notes.buildPages(changelog, ['1.1.18', '1.1.17', '1.1.16', '1.1.15'], '1.1.14');
  assert.deepEqual(keys(pages), ['new', '1.1.14', '1.1.13']);
  assert.equal(pages[0].combined, true);
  assert.equal(pages[0].since, '1.1.14');
  assert.deepEqual(pages[0].entries.map((e) => e.version), ['1.1.18', '1.1.17', '1.1.16', '1.1.15']);
  assert.equal(notes.startPage(pages, '1.1.18'), 0);
});

test('after a one-step update, the page says where it came from', () => {
  const pages = notes.buildPages(changelog, ['1.1.18'], '1.1.17');
  assert.deepEqual(keys(pages), changelog.map((e) => e.version));
  assert.equal(pages[0].since, '1.1.17');
  assert.equal(pages[1].since, undefined);
});

test('paging stops at both ends', () => {
  assert.equal(notes.stepPage(0, -1, 3), 0);
  assert.equal(notes.stepPage(0, 1, 3), 1);
  assert.equal(notes.stepPage(2, 1, 3), 2);
  assert.equal(notes.stepPage(0, 1, 0), 0);
});

// ── One launch dialog at a time ─────────────────────────────────────────────

test('launch dialogs open one after the other', () => {
  const q = createLaunchDialogQueue();
  const opened = [];
  q.request('whatsNew', () => opened.push('whatsNew'), 100);
  assert.deepEqual(opened, ['whatsNew']);
  q.request('reopen', () => opened.push('reopen'), 0);
  assert.deepEqual(opened, ['whatsNew'], 'a second dialog opened on top of the first');
  q.done('whatsNew');
  assert.deepEqual(opened, ['whatsNew', 'reopen']);
});

test('a launch dialog waits while another dialog is open, and the earlier order goes first', () => {
  const q = createLaunchDialogQueue();
  const opened = [];
  q.setBlocked(true);
  q.request('whatsNew', () => opened.push('whatsNew'), 100);
  q.request('reopen', () => opened.push('reopen'), 0);
  q.request('whatsNew', () => opened.push('duplicate'), 100);
  assert.deepEqual(opened, []);
  q.setBlocked(false);
  assert.deepEqual(opened, ['reopen']);
  // Its own flag blocks the queue while it is open; closing it frees both.
  q.setBlocked(true);
  q.done('reopen');
  assert.deepEqual(opened, ['reopen']);
  q.setBlocked(false);
  assert.deepEqual(opened, ['reopen', 'whatsNew']);
  assert.equal(q.current(), 'whatsNew');
});

test('a waiting dialog opened by hand leaves the queue', () => {
  const q = createLaunchDialogQueue();
  const opened = [];
  q.setBlocked(true);
  q.request('whatsNew', () => opened.push('whatsNew'));
  q.done('whatsNew');
  q.setBlocked(false);
  assert.deepEqual(opened, []);
});

// ── The app wiring ───────────────────────────────────────────────────────────

test('the app asks at launch, through the queue, and marks the notes seen on close', () => {
  const app = read('../src/App.svelte');
  assert.match(app, /WhatsNewOnLaunch\(\)/);
  assert.match(app, /launchDialogs\.request\('whatsNew'/);
  assert.match(app, /\$: launchDialogs\.setBlocked\(anyDialogOpen\)/);
  assert.match(app, /showWhatsNew \|\|/, 'the dialog must count as open, or the terminal takes its keys');
  assert.match(app, /MarkWhatsNewSeen\(\)/);
  assert.match(app, /<WhatsNewDialog[\s\S]*?on:close=\{handleWhatsNewClosed\}/);
});

test('it can be opened from Help, the update dialog and the command palette', () => {
  const app = read('../src/App.svelte');
  assert.match(read('../src/lib/components/Dialogs/HelpDialog.svelte'), /dispatch\('whatsNew'\)/);
  assert.match(read('../src/lib/components/Dialogs/UpdateDialog.svelte'), /dispatch\('whatsNew'\)/);
  assert.match(read('../src/lib/components/Dialogs/CommandPalette.svelte'), /command:whats-new/);
  assert.match(app, /<HelpDialog[\s\S]*?on:whatsNew=/);
  assert.match(app, /<UpdateDialog[\s\S]*?on:whatsNew=/);
  assert.match(app, /addEventListener\('command:whats-new'/);
});

test('every language has the dialog', () => {
  const en = JSON.parse(read('../src/lib/i18n/locales/en.json'));
  const wanted = Object.keys(en).filter((k) => k.startsWith('whatsNew.'));
  assert.ok(wanted.length >= 10);
  for (const lang of ['ar', 'cs', 'de', 'es', 'fr', 'hu', 'it', 'ja', 'ko', 'nl', 'pl', 'pt-br', 'ru', 'sv', 'th', 'tr', 'uk', 'vi', 'zh-cn']) {
    const locale = JSON.parse(read(`../src/lib/i18n/locales/${lang}.json`));
    for (const key of wanted) {
      assert.ok(locale[key], `${lang} is missing ${key}`);
      for (const param of en[key].match(/\{\w+\}/g) || []) {
        assert.ok(locale[key].includes(param), `${lang} ${key} lost ${param}`);
      }
    }
  }
});
