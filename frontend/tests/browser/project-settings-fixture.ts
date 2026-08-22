import { mount } from 'svelte';
import ProjectSettingsFixture from './project-settings-fixture.svelte';

let selectedProject = 'project-a';
const settingsSaves: unknown[] = [];
const backend = new Proxy({
  SelectProject: async (id: string) => { selectedProject = id; },
  GetActiveProjectID: async () => selectedProject,
  GetSessions: async () => [],
  GetGroups: async () => [],
  GetSettings: async () => {
    if (selectedProject === 'project-c') throw new Error('fixture settings unavailable');
    return { language: selectedProject === 'project-b' ? 'hu' : 'en' };
  },
  GetLockStatus: async () => ({ locked: true, otherInstancePid: 0 }),
  SaveSettings: async (value: unknown) => { settingsSaves.push(structuredClone(value)); },
  LogFrontend: async () => undefined,
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});
(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
(window as any).projectSettingsFixture = { settingsSaves: () => structuredClone(settingsSaves) };

const target = document.getElementById('fixture');
if (!target) throw new Error('project settings fixture target is missing');
mount(ProjectSettingsFixture, {
  target,
  props: {
    onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; },
  },
});
