/**
 * Turns a flat list of diff file paths into a directory tree, then flattens it
 * back into rows with a depth field.
 *
 * Rows rather than a nested component: Svelte 3 would need <svelte:self> to
 * render a nested tree, which splits the row markup across two files and makes
 * the shared bits (selection, revert button, stats) awkward to keep identical
 * to the flat list. A flat row list renders from ONE keyed {#each} in the same
 * component, so both views share the same markup conventions and the collapse
 * state is a single Set in the parent.
 */

/** Anything with a path and stats can be laid out; the diff's DiffFile fits. */
export interface TreeFileInput {
  path: string;
  added: number;
  removed: number;
}

export interface DirNode {
  /** Full path of the directory, used as the collapse key. */
  path: string;
  /** What the row shows — a collapsed chain like "src/lib/components". */
  label: string;
  dirs: DirNode[];
  files: TreeFileInput[];
  added: number;
  removed: number;
  /** Files anywhere below this directory. */
  fileCount: number;
}

export type TreeRow<T extends TreeFileInput> =
  | { kind: 'dir'; depth: number; path: string; label: string; added: number; removed: number; fileCount: number }
  | { kind: 'file'; depth: number; path: string; file: T };

/**
 * Build the directory tree. Directories keep the order in which their first
 * file appeared, so the tree follows git's own ordering rather than
 * re-sorting behind the user's back.
 */
export function buildFileTree<T extends TreeFileInput>(files: T[]): DirNode {
  const root: DirNode = { path: '', label: '', dirs: [], files: [], added: 0, removed: 0, fileCount: 0 };
  const byPath = new Map<string, DirNode>([['', root]]);

  for (const file of files) {
    const segments = file.path.split('/');
    const name = segments.pop() as string;
    // A path that is nothing but separators has no basename to show. Bail
    // before touching the tree so it can't leave an empty directory behind.
    if (!name) continue;
    // A doubled separator would otherwise create an unnamed directory row;
    // skipping empty segments keeps the tree honest.
    let node = root;
    let prefix = '';
    for (const segment of segments) {
      if (!segment) continue;
      prefix = prefix ? `${prefix}/${segment}` : segment;
      let child = byPath.get(prefix);
      if (!child) {
        child = { path: prefix, label: segment, dirs: [], files: [], added: 0, removed: 0, fileCount: 0 };
        byPath.set(prefix, child);
        node.dirs.push(child);
      }
      node = child;
    }
    node.files.push(file);
  }

  accumulate(root);
  collapseChains(root);
  return root;
}

/** Sum each directory's own files plus everything below it. */
function accumulate(node: DirNode): void {
  let added = 0;
  let removed = 0;
  let count = node.files.length;
  for (const file of node.files) {
    added += file.added;
    removed += file.removed;
  }
  for (const dir of node.dirs) {
    accumulate(dir);
    added += dir.added;
    removed += dir.removed;
    count += dir.fileCount;
  }
  node.added = added;
  node.removed = removed;
  node.fileCount = count;
}

/**
 * Merge a directory that holds nothing but a single subdirectory into that
 * subdirectory, so `src/lib/components` is one row instead of three. This is
 * what VS Code and IntelliJ do, and it is what makes a deep repo readable in a
 * narrow pane. The merged row keeps the DEEPEST path as its identity so
 * expanding/collapsing addresses the directory actually being shown.
 */
function collapseChains(node: DirNode): void {
  for (let i = 0; i < node.dirs.length; i++) {
    let dir = node.dirs[i];
    while (dir.files.length === 0 && dir.dirs.length === 1) {
      const only = dir.dirs[0];
      only.label = `${dir.label}/${only.label}`;
      dir = only;
    }
    node.dirs[i] = dir;
    collapseChains(dir);
  }
}

/**
 * Flatten the tree into rows. `collapsed` holds the paths of directories the
 * user closed — everything else is expanded, because opening a diff means
 * wanting to see what changed.
 */
export function flattenTree<T extends TreeFileInput>(
  root: DirNode,
  collapsed: Set<string> | ReadonlySet<string> = new Set<string>()
): TreeRow<T>[] {
  const rows: TreeRow<T>[] = [];
  walk(root, 0, rows, collapsed);
  return rows;
}

function walk<T extends TreeFileInput>(
  node: DirNode,
  depth: number,
  rows: TreeRow<T>[],
  collapsed: Set<string> | ReadonlySet<string>
): void {
  for (const dir of node.dirs) {
    rows.push({
      kind: 'dir',
      depth,
      path: dir.path,
      label: dir.label,
      added: dir.added,
      removed: dir.removed,
      fileCount: dir.fileCount,
    });
    if (!collapsed.has(dir.path)) walk(dir, depth + 1, rows, collapsed);
  }
  // Files after directories: a directory listing reads better with its
  // subfolders grouped at the top, the same as every file manager.
  for (const file of node.files) {
    rows.push({ kind: 'file', depth, path: file.path, file: file as T });
  }
}

/** Convenience for the view: build and flatten in one step. */
export function buildTreeRows<T extends TreeFileInput>(
  files: T[],
  collapsed: Set<string> | ReadonlySet<string> = new Set<string>()
): TreeRow<T>[] {
  return flattenTree<T>(buildFileTree(files), collapsed);
}
