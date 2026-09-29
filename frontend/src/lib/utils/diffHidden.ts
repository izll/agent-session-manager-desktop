/**
 * Files kept out of the diff view — by the view only.
 *
 * Nothing here touches git: the files keep their changes, `git status` still
 * lists them and a commit still takes them. The diff just stops offering them
 * for review, which is what a generated directory or a lock file needs.
 *
 * A rule is one of three things, always relative to the repository root:
 *
 *  - a file:    `src/app.ts`      — that path only
 *  - a folder:  `build/`          — everything under it (the trailing slash)
 *  - a pattern: `*.lock`, `gen/**` — `*` and `?` stay within one path segment,
 *               `**` crosses them. A pattern with no slash matches the file
 *               name at any depth, as in .gitignore; one with a slash is
 *               matched against the whole path. A pattern ending in a slash
 *               matches folders.
 *
 * The Go side keeps the list per repository (package diffhidden).
 */

/** The one form a rule is stored and compared in. Mirrors
 *  diffhidden.NormaliseRule, so a rule made here equals the one stored. */
export function normaliseRule(rule: string): string {
  let out = rule.replace(/\\/g, '/').trim();
  while (out.startsWith('./')) out = out.slice(2);
  out = out.replace(/^\/+/, '');
  out = out.replace(/\/{2,}/g, '/');
  return out;
}

export type RuleKind = 'file' | 'folder' | 'pattern';

export function ruleKind(rule: string): RuleKind {
  if (/[*?]/.test(rule)) return 'pattern';
  return rule.endsWith('/') ? 'folder' : 'file';
}

const globCache = new Map<string, RegExp>();

/** A glob as a regular expression over a whole string. */
function globToRegExp(glob: string): RegExp {
  let cached = globCache.get(glob);
  if (cached) return cached;
  let source = '';
  for (let at = 0; at < glob.length; at++) {
    const ch = glob[at];
    if (ch === '*') {
      if (glob[at + 1] === '*') {
        at++;
        // `**/` also matches no directory at all, so `gen/**/x` finds gen/x.
        if (glob[at + 1] === '/') {
          at++;
          source += '(?:.*/)?';
        } else {
          source += '.*';
        }
      } else {
        source += '[^/]*';
      }
    } else if (ch === '?') {
      source += '[^/]';
    } else {
      source += ch.replace(/[.+^${}()|[\]\\]/g, '\\$&');
    }
  }
  cached = new RegExp(`^${source}$`);
  globCache.set(glob, cached);
  return cached;
}

/** Every folder a path sits in, outermost first, each with its slash. */
function foldersOf(path: string): string[] {
  const out: string[] = [];
  let at = path.indexOf('/');
  while (at >= 0) {
    out.push(path.slice(0, at + 1));
    at = path.indexOf('/', at + 1);
  }
  return out;
}

/** Whether one rule hides the path. */
export function ruleMatches(rule: string, path: string): boolean {
  const kind = ruleKind(rule);
  if (kind === 'file') return path === rule;
  if (kind === 'folder') return path.startsWith(rule);

  if (rule.endsWith('/')) {
    // A folder pattern: any folder the file is in, at the depth the pattern
    // names — or, with no slash before the last, at any depth.
    const folder = rule.slice(0, -1);
    const anchored = folder.includes('/');
    const re = globToRegExp(folder);
    return foldersOf(path).some((dir) => {
      const name = dir.slice(0, -1);
      return re.test(anchored ? name : name.slice(name.lastIndexOf('/') + 1));
    });
  }
  const re = globToRegExp(rule);
  if (rule.includes('/')) return re.test(path);
  return re.test(path.slice(path.lastIndexOf('/') + 1));
}

/** The rules that hide the path — what "show again" has to remove. */
export function rulesHiding(rules: readonly string[], path: string): string[] {
  return rules.filter((rule) => ruleMatches(rule, path));
}

export function isHidden(rules: readonly string[], path: string): boolean {
  return rules.some((rule) => ruleMatches(rule, path));
}

/** The diff's files, split into the ones on show and the ones hidden. */
export function splitHidden<T extends { path: string }>(
  files: readonly T[],
  rules: readonly string[],
): { shown: T[]; hidden: T[] } {
  if (!rules.length) return { shown: files.slice(), hidden: [] };
  const shown: T[] = [];
  const hidden: T[] = [];
  for (const file of files) (isHidden(rules, file.path) ? hidden : shown).push(file);
  return { shown, hidden };
}

/**
 * The rules worth listing on their own in the Skipped tab.
 *
 * A file rule whose file is in the diff is already listed as that file; a
 * second row saying the same would only be noise. Folders, patterns, and file
 * rules for files with nothing to show right now are listed, so a rule can
 * always be found and taken back.
 */
export function standaloneRules(
  rules: readonly string[],
  hidden: readonly { path: string }[],
): string[] {
  const listed = new Set(hidden.map((file) => file.path));
  return rules.filter((rule) => !(ruleKind(rule) === 'file' && listed.has(rule)));
}

/** The folder a path is in, with its slash; '' at the root. */
export function folderOf(path: string): string {
  const at = path.lastIndexOf('/');
  return at < 0 ? '' : path.slice(0, at + 1);
}

/** "Every file of this type" as a pattern, or null for a file without one. */
export function extensionPattern(path: string): string | null {
  const name = path.slice(path.lastIndexOf('/') + 1);
  const dot = name.lastIndexOf('.');
  // A leading dot is a hidden file's name, not an extension.
  if (dot <= 0 || dot === name.length - 1) return null;
  return `*${name.slice(dot)}`;
}

/** How many of the listed files one rule hides. */
export function hiddenByCount(rule: string, files: readonly { path: string }[]): number {
  let count = 0;
  for (const file of files) if (ruleMatches(rule, file.path)) count++;
  return count;
}
