import { get } from 'svelte/store';
import { t } from '../i18n';

/**
 * Turns an error from the backend into something a user can read.
 *
 * The Go side reports failures in two shapes. Most are plain English
 * sentences, written where the failure happens and never translated — they
 * reach the user exactly as the programmer typed them. A few are translation
 * keys ("error.sessionNotFound"), a convention the codebase already had but
 * that nothing ever resolved: the key itself was printed.
 *
 * This resolves both. A key becomes its translation; anything else is passed
 * through unchanged, because an English sentence is still better than nothing.
 *
 * Values are carried in the key itself, separated by `|`, so a message can
 * name the thing it is about without the backend knowing any language:
 *
 *   error.agentNotOnPath|codex|/root/.local/bin
 *
 * fills {0} and {1} in the translated string.
 */
export function describeBackendError(error: unknown): string {
  const raw = String((error as { message?: string })?.message ?? error ?? '');

  // Wails prefixes rejected promises; the key is what follows.
  const text = raw.replace(/^Error:\s*/, '').trim();
  if (!text.startsWith('error.')) {
    return text;
  }

  const [key, ...values] = text.split('|');
  const translated = get(t)(key);

  // An unknown key comes back as itself — better to show the sentence the
  // backend sent than a bare identifier.
  if (translated === key) {
    return values.length > 0 ? `${key}: ${values.join(' ')}` : key;
  }

  return values.reduce(
    (message, value, index) => message.split(`{${index}}`).join(value),
    translated,
  );
}
