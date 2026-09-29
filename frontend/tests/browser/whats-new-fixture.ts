import { mount, tick } from 'svelte';

// The release-notes dialog over a mocked backend. ?launch=1 opens it the way
// the app does after an update from 1.1.14; without it, the way Help opens it.
const entry = (version: string, items: string[], extra: Record<string, unknown> = {}) => ({
  version, date: `2026-09-${version.split('.')[2].padStart(2, '0')}`, intro: [],
  sections: [{ kind: 'fixed', title: 'Fixed', intro: [], items }], ...extra,
});

const changelog = [
  entry('1.1.18', [], { sections: [
    { kind: 'added', title: 'Added', intro: [], items: ['**The favourites section folds.** Press `Enter` on its header.'] },
    { kind: 'fixed', title: 'Fixed', intro: [], items: [
      'Markup stays text: <img src=x onerror="window.pwned=1"><script>window.pwned=1</script>',
      'Reported in [#1](https://github.com/izll/agent-session-manager-desktop/pull/1), not [here](javascript:window.pwned=1).',
    ] },
  ] }),
  entry('1.1.17', ['Seventeen.']),
  entry('1.1.16', ['Sixteen.']),
  entry('1.1.15', ['Fifteen.']),
  entry('1.1.14', ['Fourteen.']),
  entry('1.1.13', ['Thirteen.'], { intro: ['An intro paragraph.'] }),
];

const opened: string[] = [];
(window as any).openedLinks = opened;
(window as any).go = { main: { App: {
  GetChangelog: async () => changelog,
  GetVersion: async () => '1.1.17',
} } };
(window as any).runtime = new Proxy({}, {
  get: (_t, key) => key === 'BrowserOpenURL'
    ? (url: string) => { opened.push(url); }
    : () => () => {},
});

const launch = new URLSearchParams(location.search).get('launch') === '1';

const { default: WhatsNewDialog } = await import('../../src/lib/components/Dialogs/WhatsNewDialog.svelte');
const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');

let closed = 0;
mount(WhatsNewDialog, {
  target,
  props: launch
    ? { show: true, fresh: ['1.1.18', '1.1.17', '1.1.16', '1.1.15'], since: '1.1.14' }
    : { show: true },
  events: {
    close: () => { document.body.dataset.closed = String(++closed); },
  },
});
await tick();
for (let i = 0; i < 100 && !document.querySelector('.release'); i++) {
  await new Promise((resolve) => setTimeout(resolve, 20));
}
document.body.dataset.fixtureReady = 'true';
