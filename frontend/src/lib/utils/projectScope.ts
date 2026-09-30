/**
 * The project's own task list and note — the ones that belong to no session —
 * are addressed with this in place of a session ID.
 *
 * Session IDs are generated and never start with "@". The backend has the
 * same constant (ProjectTasksScope in project_tasks_api.go), and a test keeps
 * the two equal.
 */
export const PROJECT_TASKS_SCOPE = '@project';
