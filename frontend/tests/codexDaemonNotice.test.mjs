import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { transformSync } from 'esbuild';

// A Codex conversation the background server still held is continued through
// it, where YOLO does not apply. The user is told, and offered to stop the
// server.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

const { code } = transformSync(read('../src/lib/components/common/codexDaemonNotice.ts'), {
  loader: 'ts',
  format: 'esm',
});
const { codexDaemonNoticeText } = await import(`data:text/javascript,${encodeURIComponent(code)}`);

const en = JSON.parse(read('../src/lib/i18n/locales/en.json'));
const t = (key, params = {}) =>
  (en[key] ?? key).replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? ''));
const notice = { sessionId: 's1', sessionName: 'Tickwell', serverId: '', conversationId: 'x' };

test('the notice says YOLO is not in effect and offers to stop the server', () => {
  const text = codexDaemonNoticeText(t, 'held', notice, '');
  assert.equal(text.variant, 'warning');
  assert.match(text.message, /^Tickwell: /);
  assert.match(text.message, /YOLO is not in effect/);
  assert.equal(text.action, 'Stop background server');
});

test('after stopping, it says how to get YOLO back, with no button', () => {
  const text = codexDaemonNoticeText(t, 'stopped', notice, '');
  assert.equal(text.variant, 'success');
  assert.match(text.message, /Restart the tab/);
  assert.equal(text.action, '');
});

test('a failed stop shows the error', () => {
  const text = codexDaemonNoticeText(t, 'failed', notice, 'exit status 1');
  assert.equal(text.variant, 'error');
  assert.match(text.message, /exit status 1$/);
  assert.equal(text.action, '');
});

test('the notice listens to the event the backend sends, and is mounted', () => {
  const backend = read('../../app_codex_daemon.go');
  const event = backend.match(/codexDaemonHeldEvent = "([^"]+)"/)[1];
  const component = read('../src/lib/components/common/CodexDaemonNotice.svelte');
  assert.ok(component.includes(`'${event}'`), `the component does not listen to ${event}`);
  assert.match(component, /StopCodexDaemon\(notice\.sessionId, notice\.serverId\)/);
  assert.match(read('../src/App.svelte'), /<CodexDaemonNotice \/>/);
});

test('the toast can carry a button', () => {
  const toast = read('../src/lib/components/common/Toast.svelte');
  assert.match(toast, /export let actionLabel = ''/);
  assert.match(toast, /dispatch\('action'\)/);
});

test('every locale has the notice', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  for (const file of readdirSync(dir)) {
    const locale = JSON.parse(readFileSync(new URL(file, dir), 'utf8'));
    for (const key of ['codexDaemon.held', 'codexDaemon.stop', 'codexDaemon.stopped', 'codexDaemon.stopFailed']) {
      assert.ok(locale[key], `${file} has no ${key}`);
    }
    assert.ok(locale['codexDaemon.held'].includes('{session}'), `${file}: the session name is lost`);
    assert.ok(locale['codexDaemon.stopFailed'].includes('{error}'), `${file}: the error is lost`);
  }
});
