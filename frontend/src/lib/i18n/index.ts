import { writable, derived, get } from 'svelte/store';
// Eager-load English so text renders during the initial paint instead of
// showing raw i18n keys (e.g. "app.title") until the async loader finishes.
import enLocale from './locales/en.json';

export const locale = writable<string>('en');
export const isRTL = derived(locale, ($l) => $l === 'ar');

const localeModules: Record<string, () => Promise<{ default: Record<string, string> }>> = {
  en: () => import('./locales/en.json'),
  hu: () => import('./locales/hu.json'),
  de: () => import('./locales/de.json'),
  es: () => import('./locales/es.json'),
  fr: () => import('./locales/fr.json'),
  'pt-br': () => import('./locales/pt-br.json'),
  it: () => import('./locales/it.json'),
  ru: () => import('./locales/ru.json'),
  'zh-cn': () => import('./locales/zh-cn.json'),
  ja: () => import('./locales/ja.json'),
  ko: () => import('./locales/ko.json'),
  tr: () => import('./locales/tr.json'),
  pl: () => import('./locales/pl.json'),
  nl: () => import('./locales/nl.json'),
  cs: () => import('./locales/cs.json'),
  uk: () => import('./locales/uk.json'),
  ar: () => import('./locales/ar.json'),
  th: () => import('./locales/th.json'),
  vi: () => import('./locales/vi.json'),
  sv: () => import('./locales/sv.json'),
};

let translations: Record<string, string> = enLocale as Record<string, string>;
let fallback: Record<string, string> = enLocale as Record<string, string>;

// Internal trigger to force re-derive after async load
const _tick = writable(0);

export async function loadTranslations(lang: string) {
  // Load requested locale
  if (lang === 'en') {
    translations = fallback;
  } else if (localeModules[lang]) {
    try {
      const mod = await localeModules[lang]();
      translations = mod.default;
    } catch (e) {
      console.error(`Failed to load locale ${lang}:`, e);
      translations = fallback;
    }
  } else {
    translations = fallback;
  }

  locale.set(lang);
  _tick.update(n => n + 1);
}

export const t = derived([locale, _tick], () => {
  return (key: string, params?: Record<string, string | number>): string => {
    let str = translations[key] ?? fallback[key] ?? key;
    if (params) {
      str = str.replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? ''));
    }
    return str;
  };
});
