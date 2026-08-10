import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';

/**
 * A tab's fields keep the names the storage file uses, not the UI's.
 *
 * SessionInfo is built for the frontend and renames everything to camelCase.
 * The tabs inside it are `session.FollowedWindow` — the STORED structure,
 * passed straight through — so they arrive as `resume_session_id`,
 * `work_dir`, `auto_yes` and so on.
 *
 * Reading one as camelCase is not a type error and not a crash: it is
 * `undefined`, which reads as "this tab has no conversation id" and looks
 * exactly like a tab that genuinely has none. It went unnoticed until a forked
 * tab, where the id is the whole point of the feature — and it had also been
 * silently disabling the "this conversation is already open in X" warning for
 * tabs, which is the one thing that warning exists to catch.
 *
 * This checks every camelCase spelling of a tab field against the generated
 * model, so the next one is caught when it is written rather than when someone
 * notices a blank where an id should be.
 */
const models = readFileSync(new URL('../wailsjs/go/models.ts', import.meta.url), 'utf8');

const cls = models.match(/export class FollowedWindow \{([\s\S]*?)static createFrom/);
assert.ok(cls, 'FollowedWindow is missing from the generated models');

const fields = [...cls[1].matchAll(/^\s+(\w+)\??:/gm)].map((m) => m[1]);
assert.ok(fields.includes('resume_session_id'), 'the model should carry the stored field names');

const snakeFields = fields.filter((f) => f.includes('_'));
assert.ok(snakeFields.length > 3, 'expected several snake_case fields to check');

const camelOf = (s) => s.replace(/_([a-z])/g, (_, c) => c.toUpperCase());

// Only files that actually handle tabs.
const files = execFileSync('grep', ['-rl', 'followedWindows', 'src',
  '--include=*.svelte', '--include=*.ts'], {
  encoding: 'utf8',
  cwd: new URL('..', import.meta.url).pathname,
}).trim().split('\n').filter(Boolean);

const problems = [];
for (const file of files) {
  const source = readFileSync(new URL(`../${file}`, import.meta.url), 'utf8');

  for (const field of snakeFields) {
    const camel = camelOf(field);
    // `fw.` and `f.` are the names used for a followed window; `tab.` and
    // `w.` are deliberately NOT checked, because tabStatuses is a different
    // structure built for the UI, and its fields ARE camelCase.
    const uses = [...source.matchAll(new RegExp(`\\b(fw|f)\\.${camel}\\b`, 'g'))];
    for (const use of uses) {
      const line = source.slice(0, use.index).split('\n').length;
      problems.push(`${file}:${line} reads .${camel}, but the field is .${field}`);
    }
  }
}

assert.deepEqual(problems, [], `tab fields read under the wrong name:\n  ${problems.join('\n  ')}`);

// And the two that were actually wrong, named so the fix cannot be quietly
// reverted.
const main = readFileSync(
  new URL('../src/lib/components/MainPanel/MainPanel.svelte', import.meta.url), 'utf8');
assert.match(
  main,
  /return fw\?\.resume_session_id \|\| ''/,
  'the status bar reads the tab conversation id under its stored name',
);

const dialog = readFileSync(
  new URL('../src/lib/components/Dialogs/NewSessionDialog.svelte', import.meta.url), 'utf8');
assert.match(
  dialog,
  /if \(fw\.resume_session_id === resumeId\)/,
  'the already-open warning has to match tabs too',
);

console.log('tabFieldNames: ok');
