import { writable, get } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import { afterUnsavedChanges } from './unsavedChanges';
import { dismissUndo } from './undo';

export interface Project {
  id: string;
  name: string;
  isLocked: boolean;
}

export const projects = writable<Project[]>([]);
export const activeProjectId = writable<string>('');

// PID of another instance that holds the active project's lock (0 = we own
// it). Kept in the store so the lock banner updates on every project switch,
// not just at startup.
export const otherInstancePID = writable<number>(0);
let projectSelectionQueue: Promise<void> = Promise.resolve();
let lockLoadGeneration = 0;

async function syncProjectLanguage(settingsStore: typeof import('./settings')) {
  // Settings live in the selected project's storage, while translations are
  // held in a process-global runtime module. Reloading the settings store is
  // therefore not enough: project B can say `language=hu` while every label
  // still uses project A's English dictionary.
  const { loadTranslations } = await import('../i18n');
  await loadTranslations(get(settingsStore.settings).language || 'en');
}

// Refresh the single-instance lock status for the active project.
export async function refreshLockStatus() {
  const generation = ++lockLoadGeneration;
  const projectId = get(activeProjectId);
  try {
    const lock = await App.GetLockStatus();
    if (generation !== lockLoadGeneration || projectId !== get(activeProjectId)) return;
    otherInstancePID.set(lock && !lock.locked && lock.otherInstancePid > 0 ? lock.otherInstancePid : 0);
  } catch {
    if (generation !== lockLoadGeneration || projectId !== get(activeProjectId)) return;
    otherInstancePID.set(0);
  }
}

export async function loadProjects() {
  try {
    const [projectList, currentId] = await Promise.all([
      App.GetProjects(),
      App.GetActiveProjectID()
    ]);
    projects.set(projectList as Project[]);
    activeProjectId.set(currentId);
  } catch (e) {
    console.error('Failed to load projects:', e);
    reportError(`Could not load projects: ${e}`);
  }
}

async function selectProjectNow(id: string) {
  const previousProjectId = get(activeProjectId);
  let previousSessionId: string | null = null;
  let previousWindowIdx = 0;
  let sessionStore: typeof import('./sessions') | null = null;
  let settingsStore: typeof import('./settings') | null = null;
  try {
    settingsStore = await import('./settings');
    sessionStore = await import('./sessions');
    await settingsStore.flushSettingsSaves();
    previousSessionId = get(sessionStore.selectedSessionId);
    previousWindowIdx = get(sessionStore.selectedWindowIdx);
    // Invalidate every request/mutation that started under the old backend
    // project before SelectProject changes that global backend identity.
    sessionStore.invalidateSessionProject();
    settingsStore.invalidateSettingsContext();
    // Undo closures carry project-scoped session/task ids. Keeping one while
    // the backend's implicit project target changes can restore A's snapshot
    // into an unrelated same-id session in B.
    dismissUndo();
    // Project-scoped terminal mirrors must not remain connected after the
    // backend changes its active project.
    window.dispatchEvent(new CustomEvent('terminal:destroy-all'));
    await App.SelectProject(id);
    activeProjectId.set(id);
    // Reload sessions for new project
    const { loadSessions, sessions, groups } = sessionStore;
    // Do not render the old project's session cards under the new project's
    // heading while its data is still being loaded.
    sessions.set([]);
    groups.set([]);
    await Promise.all([loadSessions(), settingsStore.loadSettings()]);
    await syncProjectLanguage(settingsStore);
    // The backend moved the lock with the switch — refresh the banner.
    await refreshLockStatus();
  } catch (e) {
    console.error('Failed to select project:', e);
    // Invalidating before SelectProject is what prevents old-project writes
    // from landing in a successfully selected replacement. If selection
    // itself fails, however, those writes were backend-visible in the still
    // active project and deliberately skipped in the UI. Re-read that project
    // before returning the failure so its store is not left stale.
    if (sessionStore && settingsStore) {
      try {
        const currentId = await App.GetActiveProjectID();
        activeProjectId.set(currentId || previousProjectId);
        await Promise.all([
          sessionStore.loadSessions(),
          settingsStore.loadSettings(),
          refreshLockStatus(),
        ]);
        await syncProjectLanguage(settingsStore);
        // A failed switch leaves the backend on the old project. Restore its
        // selection without calling selectSession(), whose persistence side
        // effects would race the recovery and are unnecessary here.
        if (previousSessionId && get(sessionStore.sessions).some((session) => session.id === previousSessionId)) {
          sessionStore.selectedSessionId.set(previousSessionId);
          sessionStore.selectedWindowIdx.set(previousWindowIdx);
        }
      } catch (recoveryError) {
        console.error('Failed to recover after project selection error:', recoveryError);
      }
    }
    throw e;
  }
}

export function selectProject(id: string): Promise<boolean> {
  // Project switching changes the implicit target of every Wails call. Wait
  // for editors to approve and actually discard their buffers before even
  // queueing the backend switch; cancellation resolves without changing the
  // project so callers can release their busy UI.
  return new Promise<boolean>((resolve, reject) => {
    afterUnsavedChanges(() => {
      const selection = projectSelectionQueue
        .catch(() => {})
        .then(() => selectProjectNow(id));
      projectSelectionQueue = selection;
      selection.then(() => resolve(true), reject);
    }, () => resolve(false));
  });
}

export async function createProject(name: string) {
  try {
    const project = await App.CreateProject(name);
    if (project) {
      projects.update(p => [...p, project as Project]);
    }
    return project;
  } catch (e) {
    console.error('Failed to create project:', e);
    throw e;
  }
}

export async function deleteProject(id: string) {
  try {
    await App.DeleteProject(id);
    projects.update(p => p.filter(proj => proj.id !== id));

    // If deleted project was active, switch to default
    if (get(activeProjectId) === id) {
      await selectProject('');
    }
  } catch (e) {
    console.error('Failed to delete project:', e);
    throw e;
  }
}
