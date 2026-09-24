import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { execSync } from 'node:child_process';

const root = new URL('../../', import.meta.url).pathname;
const localeDir = new URL('../src/lib/i18n/locales/', import.meta.url).pathname;

// Every key the Go side reports must exist in every locale.
//
// A key with no translation is worse than an English sentence: the user is
// shown the identifier itself — "error.sessionNotRunning" — which means
// nothing to anyone. Two such keys shipped this way before this test existed.
test('every error key the backend reports has a translation', () => {
  const used = new Set();
  const goText = execSync(
    // Not .claude: agent worktrees live there, each a full copy of the repo
    // with work in progress, and their keys are not this checkout's.
    `grep -rho 'error\\.[a-zA-Z][a-zA-Z0-9]*' --include='*.go' --exclude-dir=.claude --exclude-dir=node_modules ${root} || true`,
    { encoding: 'utf8' },
  );
  for (const key of goText.split('\n')) {
    const trimmed = key.trim();
    // Keys are reported from Go source; anything ending in a Go field access
    // (error.Error) is not one of ours.
    if (trimmed.startsWith('error.') && !/^error\.(Error|Is|As)$/.test(trimmed)) {
      used.add(trimmed);
    }
  }
  assert.ok(used.size > 0, 'no error keys found at all — has the convention changed?');

  const en = JSON.parse(readFileSync(localeDir + 'en.json', 'utf8'));
  const missing = [...used].filter(key => !(key in en)).sort();
  assert.deepEqual(missing, [],
    'these keys are reported by Go but have no English text, so the user sees the key itself');
});

// And every locale carries the same keys, so no language falls back to an
// identifier.
test('no locale is missing a key another has', () => {
  const files = readdirSync(localeDir).filter(name => name.endsWith('.json'));
  const en = JSON.parse(readFileSync(localeDir + 'en.json', 'utf8'));
  const enKeys = Object.keys(en).sort();

  for (const file of files) {
    if (file === 'en.json') continue;
    const other = JSON.parse(readFileSync(localeDir + file, 'utf8'));
    const missing = enKeys.filter(key => !(key in other));
    assert.deepEqual(missing, [], `${file} is missing keys`);

    const extra = Object.keys(other).filter(key => !(key in en)).sort();
    assert.deepEqual(extra, [], `${file} has keys English does not`);
  }
});

// The detail keys the connection test reports follow the same rule.
test('every step detail key has a translation', () => {
  const used = new Set();
  const goText = execSync(
    `grep -rho 'detail\\.[a-zA-Z][a-zA-Z0-9]*' --include='*.go' ${root} || true`,
    { encoding: 'utf8' },
  );
  for (const key of goText.split('\n')) {
    const trimmed = key.trim();
    if (trimmed.startsWith('detail.')) used.add(trimmed);
  }

  const en = JSON.parse(readFileSync(localeDir + 'en.json', 'utf8'));
  const missing = [...used].filter(key => !(key in en)).sort();
  assert.deepEqual(missing, [], 'these detail keys have no text');
});
