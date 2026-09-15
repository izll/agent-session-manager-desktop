import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const termSrc = readFileSync(
  new URL('../src/lib/components/MainPanel/Terminal.svelte', import.meta.url), 'utf8');
const en = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/en.json', import.meta.url), 'utf8'));
const hu = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/hu.json', import.meta.url), 'utf8'));

// What tmux leaves in a parked pane is the bare words "Pane is dead". That
// reads as a crash, not as a tab waiting to be started — which is what it is.
test('a parked tab gets the placeholder, not tmux\'s dead-pane text', () => {
  const at = termSrc.indexOf('$: parkedTab =');
  assert.ok(at > 0, 'parkedTab is gone');
  const fn = termSrc.slice(at, termSrc.indexOf('})();', at));

  assert.match(fn, /mainWindowStopped/,
    'window 0 is the session\'s own agent and has its own flag; without it a ' +
    'parked main window still shows the dead pane');
  assert.match(fn, /followedWindows\?\.find/, 'the tabs are not consulted');
  assert.match(fn, /status !== 'running'/,
    'a stopped session would be called parked, taking over the idle placeholder');
});

test('the placeholder is shown over the attached pane, not only when detached', () => {
  const at = termSrc.indexOf('$: showPlaceholder =');
  const line = termSrc.slice(at, termSrc.indexOf('\n', at));
  assert.match(line, /!isAttached \|\| parkedTab/,
    'the placeholder still only appears when nothing is attached, so the ' +
    'parked pane keeps showing "Pane is dead"');
});

// The idle placeholder floats over an empty pane; this one has tmux text
// underneath and has to cover it.
test('the parked placeholder paints over the pane', () => {
  const at = termSrc.indexOf('.terminal-placeholder.parked');
  assert.ok(at > 0, 'the parked variant has no style of its own');
  const rule = termSrc.slice(at, termSrc.indexOf('}', at));
  assert.match(rule, /background:/, 'nothing covers the dead-pane text underneath');
  // --ui-bg-base was invented for this rule and defined nowhere, so the
  // fallback colour was what actually painted, and it did not match the pane.
  assert.match(rule, /--xterm-background/,
    'the cover colour is not the terminal\'s own, so the parked pane does not ' +
    'match the tab beside it');
});

test('the wait is animated, and stops for reduced motion', () => {
  assert.match(termSrc, /@keyframes parked-breath/, 'the icon does not animate');
  assert.match(termSrc, /\.placeholder-icon\.parked \{[^}]*animation:/,
    'the animation is not on the placeholder icon');
  assert.match(termSrc, /prefers-reduced-motion: reduce/,
    'the animation runs regardless of the viewer\'s motion setting');
});

// The point of the report: a parked tab was showing something of its own
// shape — dots — where every other empty pane in the app shows a big icon
// above a line of monospace text. It has to be the same placeholder, with a
// different icon and message.
test('the parked state uses the same placeholder shape as a stopped session', () => {
  const at = termSrc.indexOf('{#if showPlaceholder}');
  const block = termSrc.slice(at, termSrc.indexOf('</div>', at));

  // One markup for both states: a second branch is how the two shapes came to
  // differ in the first place.
  assert.ok(!/\{#if parkedTab\}/.test(block),
    'the parked state has a branch of its own again, which is what let it ' +
    'drift away from the placeholder every other empty pane shows');
  assert.match(block, /class="placeholder-icon" class:parked=\{parkedTab\}/,
    'the parked icon is not the placeholder icon');
  assert.match(block, /class="placeholder-msg"/, 'the message is not the placeholder message');
  assert.ok(!/parked-dots/.test(termSrc), 'the dots are still in the component');
});

// What differs between the two states is the pair, not the markup.
test('the icon and message are chosen in one place', () => {
  assert.match(termSrc, /\$: placeholderIcon = parkedTab \? parkedIcon : placeholderIcons\[placeholderIdx\]/,
    'the icon choice is gone, or is made somewhere other than the one place');
  assert.match(termSrc, /\$: placeholderKey = parkedTab \? 'terminal\.tabParked' : placeholderKeys\[placeholderIdx\]/,
    'the message choice is gone, or is made somewhere other than the one place');
});

test('the message is translated, not hardcoded', () => {
  assert.match(termSrc, /'terminal\.tabParked'/, 'the message bypasses i18n');
  assert.match(termSrc, /\$t\(placeholderKey\)/, 'the message is not rendered through i18n');
  assert.ok(en['terminal.tabParked'], 'the English string is missing');
  assert.ok(hu['terminal.tabParked'], 'the Hungarian string is missing');
  assert.notEqual(en['terminal.tabParked'], hu['terminal.tabParked'],
    'the Hungarian string was left in English');
});
