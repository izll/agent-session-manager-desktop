// Where each visible toast sits in the shared lane down the right-hand side.
//
// Several Toast components are mounted independently — session errors, tab
// errors, folder errors, dictation — and none of them knows about the others.
// Without somewhere to agree, two showing at once are positioned at the same
// fixed point and draw on top of each other, which is how two unrelated error
// messages became one unreadable block of overlapping text.
//
// A module-level set rather than a store: this is not application state, it is
// a seat allocation that lives exactly as long as the toasts do.

/** Vertical pitch of one slot, in px: the toast's height plus a gap. */
export const toastSlotHeight = 64;

const taken = new Set<number>();

/** claimToastSlot reserves the lowest free slot and returns it. */
export function claimToastSlot(): number {
  let slot = 0;
  while (taken.has(slot)) slot++;
  taken.add(slot);
  return slot;
}

/** releaseToastSlot frees a slot for the next toast. */
export function releaseToastSlot(slot: number): void {
  taken.delete(slot);
}
