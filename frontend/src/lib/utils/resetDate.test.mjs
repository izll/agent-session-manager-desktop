// A usage window's reset time was shown as the time alone. That is enough for
// the five-hour window, which resets later today, and ambiguous for anything
// longer: Codex's weekly limit resets days away, and "14:30" says nothing about
// which day.
//
// The date is added only when the reset is not today, so the common case stays
// uncluttered.
//
// Mirrors formatResetDate in ProjectDashboard.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

function isSameDay(date, now) {
  return date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();
}

const NOW = new Date(2026, 7, 5, 10, 0);   // 5 August 2026, 10:00

test('a reset later today needs no date', () => {
  assert.ok(isSameDay(new Date(2026, 7, 5, 14, 30), NOW));
});

test('a reset tomorrow does', () => {
  assert.equal(isSameDay(new Date(2026, 7, 6, 9, 0), NOW), false);
});

test('the same clock time on another day is not today', () => {
  // The case the time-only display could not distinguish at all.
  assert.equal(isSameDay(new Date(2026, 7, 12, 10, 0), NOW), false);
});

test('the same day-of-month in another month is not today', () => {
  assert.equal(isSameDay(new Date(2026, 8, 5, 10, 0), NOW), false);
});

test('the same date in another year is not today', () => {
  assert.equal(isSameDay(new Date(2027, 7, 5, 10, 0), NOW), false);
});

test('midnight and just before it fall on different days', () => {
  // A five-hour window opened in the evening resets after midnight, which is
  // exactly where the time alone misleads.
  const late = new Date(2026, 7, 5, 23, 59);
  const justAfter = new Date(2026, 7, 6, 0, 30);
  assert.ok(isSameDay(late, NOW));
  assert.equal(isSameDay(justAfter, NOW), false);
});
