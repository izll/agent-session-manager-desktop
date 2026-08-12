/**
 * Converting between a task's stored deadline and what a date field shows.
 *
 * The two formats are not the same thing and the difference is a whole
 * timezone offset. The task stores RFC 3339 with a zone ("2026-08-20T14:30:00Z"
 * or "...+02:00"); <input type="datetime-local"> takes and gives
 * "YYYY-MM-DDTHH:mm" with no zone at all, interpreted as the viewer's local
 * time.
 *
 * Slicing the string is the obvious shortcut and it is wrong: for anyone not on
 * UTC it shows a deadline set for 14:30 as 12:30, and saving it back moves the
 * deadline by that offset every single time the task is edited.
 */

/** Two digits, because a date field rejects "2026-8-3T9:05". */
function pad(value: number): string {
  return String(value).padStart(2, '0');
}

/**
 * RFC 3339 → the value a datetime-local field displays, in local time.
 *
 * Returns "" for anything unparseable, which leaves the field empty rather than
 * showing "Invalid Date" or, worse, a plausible wrong date.
 */
export function toLocalInputValue(rfc3339: string): string {
  if (!rfc3339) return '';

  const date = new Date(rfc3339);
  if (Number.isNaN(date.getTime())) return '';

  // Local getters throughout: the field means local time, and the whole point
  // of this function is to do that conversion rather than pretend it away.
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/**
 * The datetime-local field's value → RFC 3339 for storage.
 *
 * An empty field means "no deadline" and gives "", which the backend takes as
 * an instruction to clear it — distinct from omitting the field, which leaves
 * an existing deadline alone.
 */
export function fromLocalInputValue(local: string): string {
  if (!local) return '';

  // new Date("2026-08-20T14:30") parses as local time, which is what the field
  // meant. toISOString then yields UTC, so the stored value is unambiguous
  // wherever it is read back.
  const date = new Date(local);
  if (Number.isNaN(date.getTime())) return '';

  return date.toISOString();
}

/** Milliseconds in a day, for the "due soon" window below. */
const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * How a deadline should be shown: overdue, due soon, or neither.
 *
 * Finished tasks are never late — the deadline stopped mattering when the work
 * was done, and marking them red forever makes the colour meaningless.
 */
export function deadlineState(
  rfc3339: string | undefined,
  status: string,
  now: Date = new Date(),
): 'none' | 'overdue' | 'soon' | 'later' {
  if (!rfc3339) return 'none';
  if (status === 'done') return 'none';

  const due = new Date(rfc3339);
  if (Number.isNaN(due.getTime())) return 'none';

  const remaining = due.getTime() - now.getTime();
  if (remaining < 0) return 'overdue';
  if (remaining <= DAY_MS) return 'soon';
  return 'later';
}
