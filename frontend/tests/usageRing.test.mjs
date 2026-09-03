import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The ring's colour compares spending against elapsed time, not against a fixed
// threshold: half the quota gone with half the window left is fine, half of it
// gone in the first hour is not, and a threshold at 50% cannot tell those apart.
function ringState(percent, timePercent) {
  const clamped = Math.max(0, Math.min(100, percent));
  if (timePercent < 0) {
    return clamped < 50 ? 'ok' : clamped < 80 ? 'warn' : 'over';
  }
  if (clamped >= 100 || clamped > timePercent) return 'over';
  if (clamped > timePercent * 0.75) return 'warn';
  return 'ok';
}

test('spending ahead of the clock is flagged', () => {
  assert.equal(ringState(60, 20), 'over', '60% of the quota one fifth in is trouble');
});

test('spending in step with the clock is fine', () => {
  assert.equal(ringState(50, 90), 'ok', 'half the quota with the window nearly over is comfortable');
});

test('approaching the clock warns before it passes it', () => {
  assert.equal(ringState(80, 100), 'warn');
});

test('a full quota is over regardless of the clock', () => {
  assert.equal(ringState(100, 100), 'over');
});

test('with no reset time known it falls back to thresholds', () => {
  assert.equal(ringState(30, -1), 'ok');
  assert.equal(ringState(60, -1), 'warn');
  assert.equal(ringState(90, -1), 'over');
});

test('a percentage outside 0-100 is clamped rather than drawn past the ring', () => {
  assert.equal(ringState(140, -1), 'over');
  assert.equal(ringState(-5, -1), 'ok');
});

// The gate is what makes "off" mean off. It lives in the backend so that
// switching a ring off stops the request, not just the drawing.
test('the header asks the gated endpoint, not the raw one', () => {
  const src = readFileSync(
    new URL('../src/App.svelte', import.meta.url), 'utf8');
  assert.match(src, /GetUsageRings\(\)/, 'the header no longer uses the gated endpoint');
  assert.doesNotMatch(src, /GetClaudeUsage\(\)/,
    'calling the ungated endpoint would fetch Claude usage even with the ring off');
});

test('the poll is cleared when the sidebar goes away', () => {
  const src = readFileSync(
    new URL('../src/App.svelte', import.meta.url), 'utf8');
  const destroy = src.slice(src.indexOf('onDestroy(()'));
  assert.match(destroy.slice(0, 400), /clearInterval\(usageTimer\)/,
    'the usage poll would outlive the app');
});

// The two Claude windows are separate switches, not a choice: they answer
// different questions and are usually wanted together.
test('both Claude rings can be drawn at once', () => {
  const src = readFileSync(
    new URL('../src/App.svelte', import.meta.url), 'utf8');
  assert.match(src, /showFiveHour/, 'the five-hour ring is no longer drawn');
  assert.match(src, /showSevenDay/, 'the seven-day ring is no longer drawn');
  assert.doesNotMatch(src, /usageRings\?\.window === '7d'/,
    'the windows are still treated as an either/or choice');
});

test('the settings offer a switch per window rather than a dropdown', () => {
  const dialog = readFileSync(
    new URL('../src/lib/components/Dialogs/SettingsDialog.svelte', import.meta.url), 'utf8');
  assert.match(dialog, /showClaudeFiveHourRing/);
  assert.match(dialog, /showClaudeSevenDayRing/);
  assert.doesNotMatch(dialog, /usageRingWindow/,
    'the exclusive window dropdown is still there');
});
