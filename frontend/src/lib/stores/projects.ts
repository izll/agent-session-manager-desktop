import { writable, get } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export interface Project {
  id: string;
  name: string;
  isLocked: boolean;
}

export const projects = writable<Project[]>([]);
export const activeProjectId = writable<string>('');
export const isLoadingProjects = writable<boolean>(false);

export async function loadProjects() {
  isLoadingProjects.set(true);
  try {
    const [projectList, currentId] = await Promise.all([
      App.GetProjects(),
      App.GetActiveProjectID()
    ]);
    projects.set(projectList as Project[]);
    activeProjectId.set(currentId);
  } catch (e) {
    console.error('Failed to load projects:', e);
  } finally {
    isLoadingProjects.set(false);
  }
}

export async function selectProject(id: string) {
  try {
    await App.SelectProject(id);
    activeProjectId.set(id);
    // Reload sessions for new project
    const { loadSessions, sessions, groups, selectSession } = await import('./sessions');
    // Do not render the old project's session cards under the new project's
    // heading while its data is still being loaded.
    sessions.set([]);
    groups.set([]);
    selectSession(null);
    await loadSessions();
  } catch (e) {
    console.error('Failed to select project:', e);
    throw e;
  }
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
