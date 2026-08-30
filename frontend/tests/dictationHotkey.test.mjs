import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The module is TypeScript; strip the types so node can run the real code
// rather than a reimplementation of it.
const src = readFileSync(new URL('../src/lib/utils/dictationHotkey.ts', import.meta.url), 'utf8')
  .replace(/export interface[\s\S]*?\n}\n/, '')
  .replace(/: Partial<DictationHotkey> \| null \| undefined/g, '')
  .replace(/: DictationHotkey \| null/g, '')
  .replace(/: KeyboardEvent/g, '')
  .replace(/: boolean/g, '')
  .replace(/: void/g, '')
  .replace(/: string/g, '')
  .replace(/let current = null;/, 'let current = null;')
  .replace(/export /g, '');
const mod = new Function(`${src}; return { setDictationHotkey, getDictationHotkey, matchesDictationHotkey };`)();
const { setDictationHotkey, matchesDictationHotkey } = mod;

const press = (o) => ({ ctrlKey: false, altKey: false, shiftKey: false, key: '', ...o });

test('the bound combination is recognised', () => {
  setDictationHotkey({ ctrl: true, alt: true, shift: false, key: 'g' });
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, altKey: true, key: 'g' })), true);
});

test('a different key with the same modifiers is not the hotkey', () => {
  setDictationHotkey({ ctrl: true, alt: true, shift: false, key: 'g' });
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, altKey: true, key: 'c' })), false);
});

test('the same key without the modifiers reaches the pane', () => {
  setDictationHotkey({ ctrl: true, alt: true, shift: false, key: 'g' });
  assert.equal(matchesDictationHotkey(press({ key: 'g' })), false,
    'a bare g must still be typed into the terminal');
});

test('an extra modifier is a different combination', () => {
  setDictationHotkey({ ctrl: true, alt: true, shift: false, key: 'g' });
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, altKey: true, shiftKey: true, key: 'g' })), false);
});

test('Ctrl+C is never swallowed — that would break interrupting an agent', () => {
  setDictationHotkey({ ctrl: true, alt: true, shift: false, key: 'g' });
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, key: 'c' })), false);
});

test('rebinding takes effect and the old combination passes through again', () => {
  setDictationHotkey({ ctrl: true, alt: true, shift: false, key: 'g' });
  setDictationHotkey({ ctrl: true, shift: true, alt: false, key: 'd' });
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, altKey: true, key: 'g' })), false,
    'the old binding should no longer be filtered');
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, shiftKey: true, key: 'd' })), true);
});

test('an unset hotkey filters nothing', () => {
  setDictationHotkey(null);
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, altKey: true, key: 'g' })), false);
});

test('a modifierless binding is ignored rather than eating every keystroke', () => {
  setDictationHotkey({ ctrl: false, alt: false, shift: false, key: 'g' });
  assert.equal(matchesDictationHotkey(press({ key: 'g' })), false,
    'a bare letter binding must not stop the letter being typed');
});

test('case does not matter — Shift or caps lock still matches', () => {
  setDictationHotkey({ ctrl: true, alt: true, shift: false, key: 'g' });
  assert.equal(matchesDictationHotkey(press({ ctrlKey: true, altKey: true, key: 'G' })), true);
});
