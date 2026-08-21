/** A component-owned draft that must explicitly approve destructive navigation. */
export type UnsavedGuard = {
  isDirty: () => boolean;
  /**
   * Identity of the draft state covered by the current confirmation.
   *
   * A destructive action can wait on several editors in sequence. If an
   * already-approved editor changes again while the next confirmation is
   * open, that is a new draft and must be confirmed again. Guards without a
   * revision retain the legacy one-approval-per-run behaviour.
   */
  revision?: () => unknown;
  requestDiscard: (continueAfterDiscard: () => void, cancelDiscard?: () => void) => void;
};

const guards = new Set<UnsavedGuard>();

export function registerUnsavedGuard(guard: UnsavedGuard): () => void {
  guards.add(guard);
  return () => guards.delete(guard);
}

/**
 * Continue only after every currently dirty component has confirmed discard.
 * Guards are component-owned because only the editor can present the right
 * recovery choice and clear its buffer without leaving stale UI behind.
 */
export function afterUnsavedChanges(action: () => void, onCancel: () => void = () => {}): void {
  const approved = new Map<UnsavedGuard, unknown>();
  const revisionOf = (guard: UnsavedGuard) => guard.revision ? guard.revision() : guard;
  const next = () => {
    // Re-scan the live registry after every approval. A second editor may
    // become dirty while an earlier confirmation is open.
    const guard = [...guards].find((candidate) =>
      candidate.isDirty() && (!approved.has(candidate) || approved.get(candidate) !== revisionOf(candidate))
    );
    if (!guard) {
      action();
      return;
    }
    guard.requestDiscard(() => {
      // Editors may remain dirty until their modal is torn down. Remembering
      // the approval avoids prompting the same guard forever in that case.
      approved.set(guard, revisionOf(guard));
      next();
    }, onCancel);
  };
  next();
}
