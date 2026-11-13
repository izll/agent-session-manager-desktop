/** The notice for a saved command that failed in the background, which has no tab. */

export type BackgroundFailure = { name: string; exitCode: number; output: string };

/** Its name, exit code and the last few lines it printed. */
export function describeBackgroundFailure(
  failure: BackgroundFailure,
  t: (key: string, params?: Record<string, string>) => string,
): string {
  const lines = (failure.output || '').split('\n').filter((line) => line.trim() !== '').slice(-4);
  return [t('commands.backgroundFailed', { name: failure.name, code: String(failure.exitCode) }), ...lines].join('\n');
}

/** Adds a failure to the notice still on screen, so one does not hide another. */
export function joinBackgroundFailures(shown: string, next: string): string {
  return shown ? `${shown}\n\n${next}` : next;
}
