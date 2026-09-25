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
const { codexDaemonNoticeText, codexDaemonNoticeWindow } = await import(`data:text/javascript,${encodeURIComponent(code)}`);

const en = JSON.parse(read('../src/lib/i18n/locales/en.json'));
const t = (key, params = {}) =>
  (en[key] ?? key).replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? ''));
const notice = { sessionId: 's1', sessionName: 'Tickwell', serverId: '', conversationId: 'x', windowIdx: 3, projectId: 'p1' };

test('the notice says YOLO is not in effect and offers to stop the server', () => {
  const text = codexDaemonNoticeText(t, 'held', notice, '');
  assert.equal(text.variant, 'warning');
  assert.match(text.message, /^Tickwell: /);
  assert.match(text.message, /YOLO is not in effect/);
  assert.equal(text.action, 'Stop background server');
});

test('after stopping, it offers to restart the tab', () => {
  const text = codexDaemonNoticeText(t, 'stopped', notice, '', true);
  assert.equal(text.variant, 'success');
  assert.match(text.message, /Restart the tab/);
  assert.equal(text.action, 'Restart tab');
});

test('with no tab to restart, it says how to get YOLO back, with no button', () => {
  const text = codexDaemonNoticeText(t, 'stopped', notice, '', false);
  assert.match(text.message, /Restart the tab/);
  assert.equal(text.action, '');
});

test('after the restart it says so; a failed restart shows the error', () => {
  const done = codexDaemonNoticeText(t, 'restarted', notice, '', true);
  assert.equal(done.variant, 'success');
  assert.match(done.message, /restarted without Codex's background server/);
  assert.equal(done.action, '');
  const failed = codexDaemonNoticeText(t, 'restartFailed', notice, 'window 3 not found', true);
  assert.equal(failed.variant, 'error');
  assert.match(failed.message, /window 3 not found$/);
  assert.equal(failed.action, '');
});

// The window the notice restarts: the one the conversation was started in,
// while it still is a Codex tab of that running session in this project.
const codexTab = (index) => ({ index, agent: 'codex' });
const session = (over = {}) => ({
  id: 's1', agent: 'claude', status: 'running', mainWindowIndex: 0,
  followedWindows: [{ index: 1, agent: 'terminal' }, codexTab(3)], ...over,
});

test('the tab to restart is the window the conversation was started in', () => {
  assert.equal(codexDaemonNoticeWindow(notice, [session()], 'p1'), 3);
});

test('a Codex main window is restarted as the main window', () => {
  const main = { ...notice, windowIdx: 0 };
  assert.equal(codexDaemonNoticeWindow(main, [session({ agent: 'codex', followedWindows: [] })], 'p1'), 0);
});

test('nothing is restarted that is no longer the same Codex tab', () => {
  assert.equal(codexDaemonNoticeWindow(notice, [session()], 'p2'), null, 'another project is in front');
  assert.equal(codexDaemonNoticeWindow(notice, [], 'p1'), null, 'the session is gone');
  assert.equal(codexDaemonNoticeWindow(notice, [session({ status: 'stopped' })], 'p1'), null, 'the session stopped');
  assert.equal(codexDaemonNoticeWindow(notice, [session({ followedWindows: [] })], 'p1'), null, 'the tab was deleted');
  assert.equal(codexDaemonNoticeWindow({ ...notice, windowIdx: 1 }, [session()], 'p1'), null, 'a terminal now has the index');
  assert.equal(codexDaemonNoticeWindow({ ...notice, windowIdx: 0 }, [session()], 'p1'), null, 'the main window is Claude');
  assert.equal(codexDaemonNoticeWindow(null, [session()], 'p1'), null);
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
  // The restart is the tab bar's own, so the terminal is rebuilt as after any restart.
  assert.match(component, /restartTab\(restarting\.sessionId, restartWindow\)/);
  assert.match(component, /codexDaemonNoticeWindow\(notice, \$sessions, \$activeProjectId\)/);
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
    for (const key of ['codexDaemon.held', 'codexDaemon.stop', 'codexDaemon.stopped', 'codexDaemon.stopFailed',
      'codexDaemon.restart', 'codexDaemon.restarted', 'codexDaemon.restartFailed']) {
      assert.ok(locale[key], `${file} has no ${key}`);
    }
    assert.ok(locale['codexDaemon.held'].includes('{session}'), `${file}: the session name is lost`);
    assert.ok(locale['codexDaemon.stopFailed'].includes('{error}'), `${file}: the error is lost`);
    assert.ok(locale['codexDaemon.restartFailed'].includes('{error}'), `${file}: the restart error is lost`);
  }
});
