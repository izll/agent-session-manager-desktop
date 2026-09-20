import { describeBackendError } from './backendError';

/**
 * Makes every failure from the Go side arrive already translated.
 *
 * The backend reports the failures a user is expected to act on as translation
 * keys — "error.agentNotOnServerPath|codex" — because the text belongs in the
 * user's language, not in the Go source. Something has to turn that back into
 * a sentence.
 *
 * Doing it at each place an error is shown does not work: there are around a
 * hundred and fifty of them, every one a chance to forget, and a forgotten one
 * shows the user a bare identifier. So it is done once, here, by wrapping the
 * bound methods themselves — every call goes through this, whoever makes it
 * and however they render what comes back.
 *
 * Only the message is rewritten. The rejection stays a rejection, the error
 * stays an Error, and code that inspects it is unaffected.
 */
export function translateBackendErrors(): void {
  const go = (window as any)?.go;
  if (!go || typeof go !== 'object') {
    // No bindings: a browser preview, or a test. Nothing to wrap.
    return;
  }

  for (const namespace of Object.values<any>(go)) {
    if (!namespace || typeof namespace !== 'object') continue;
    for (const struct of Object.values<any>(namespace)) {
      if (!struct || typeof struct !== 'object') continue;
      for (const [name, method] of Object.entries<any>(struct)) {
        if (typeof method !== 'function') continue;
        struct[name] = wrap(method, struct);
      }
    }
  }
}

function wrap(method: (...args: unknown[]) => unknown, owner: object) {
  return function (...args: unknown[]) {
    let result: unknown;
    try {
      result = method.apply(owner, args);
    } catch (error) {
      throw retext(error);
    }
    // A binding returns a promise; a synchronous throw is handled above.
    if (result && typeof (result as Promise<unknown>).catch === 'function') {
      return (result as Promise<unknown>).catch((error: unknown) => {
        throw retext(error);
      });
    }
    return result;
  };
}

/** retext replaces an error's message with its translation. */
function retext(error: unknown): unknown {
  const described = describeBackendError(error);

  if (error instanceof Error) {
    // Rewritten in place so nothing that already holds this error sees a
    // different object, and so a stack trace is not thrown away.
    error.message = described;
    return error;
  }
  // Wails rejects with a plain string for some failures.
  return typeof error === 'string' ? described : error;
}
