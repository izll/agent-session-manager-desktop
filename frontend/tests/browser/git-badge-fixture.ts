import { mount, tick } from 'svelte';
import GitBranchBadge from '../../src/lib/components/common/GitBranchBadge.svelte';
import { gitBranch } from '../../src/lib/stores/gitBranch';

// Both badges, as in the app: the header's and the status bar's.
const backend = new Proxy({}, { get: () => async () => undefined });
(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

gitBranch.set({
  path: '/repo', repository: true, branch: 'master', upstream: 'origin/master',
  behind: 2, unpushed: 9, unpushedKnown: true,
});

for (const [id, variant] of [['header', 'header'], ['status', 'statusbar']] as const) {
  const target = document.getElementById(id);
  if (!target) throw new Error(`fixture target ${id} is missing`);
  mount(GitBranchBadge, { target, props: { variant } });
}
await tick();
document.body.dataset.fixtureReady = 'true';
