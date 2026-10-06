// A path relative to one folder, as a path relative to another.
//
// The diff can show a folder of its own choosing while the Files view shows the
// tab's directory, so a file the diff names has to be found again from there —
// and may not be there at all.

function normalise(path: string): string {
  return path.replace(/\\/g, '/').replace(/\/+$/, '');
}

/**
 * `path`, relative to `fromRoot`, as a path relative to `toRoot`; '' for
 * `toRoot` itself. Null when it lies outside `toRoot`.
 */
export function rebasePath(path: string, fromRoot: string, toRoot: string): string | null {
  const from = normalise(fromRoot);
  const to = normalise(toRoot);
  if (!from || !to) return null;
  const relative = normalise(path).replace(/^\/+/, '');
  const absolute = relative ? `${from}/${relative}` : from;
  if (absolute === to) return '';
  if (!absolute.startsWith(`${to}/`)) return null;
  return absolute.slice(to.length + 1);
}
