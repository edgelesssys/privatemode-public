import { browser } from '$app/env';
import { derived, get, writable } from 'svelte/store';
import { de } from './locales/de';
import { en, type TranslationKey } from './locales/en';

export type Locale = 'en' | 'de';
export type LocalePreference = 'system' | Locale;
export type TranslationValues = Record<string, string | number>;

const STORAGE_KEY = 'privatemode_locale';
const translations = { en, de };

function systemLocale(): Locale {
  if (!browser) return 'en';
  return navigator.language.toLowerCase().startsWith('de') ? 'de' : 'en';
}

function storedPreference(): LocalePreference {
  if (!browser) return 'system';
  const stored = localStorage.getItem(STORAGE_KEY);
  return stored === 'en' || stored === 'de' || stored === 'system'
    ? stored
    : 'system';
}

const preferenceStore = writable<LocalePreference>(storedPreference());

export const localePreference = {
  subscribe: preferenceStore.subscribe,
  set(value: LocalePreference) {
    preferenceStore.set(value);
    if (browser) localStorage.setItem(STORAGE_KEY, value);
  },
};

export const locale = derived(preferenceStore, ($preference) =>
  $preference === 'system' ? systemLocale() : $preference,
);

function interpolate(message: string, values: TranslationValues = {}): string {
  return message.replace(/\{(\w+)\}/g, (_, key: string) =>
    values[key] === undefined ? `{${key}}` : String(values[key]),
  );
}

export function translate(
  key: TranslationKey,
  values?: TranslationValues,
  selectedLocale: Locale = get(locale),
): string {
  return interpolate(translations[selectedLocale][key] ?? en[key], values);
}

export const t = derived(
  locale,
  ($locale) => (key: TranslationKey, values?: TranslationValues) =>
    translate(key, values, $locale),
);

export function initLocale(): () => void {
  if (!browser) return () => {};
  return locale.subscribe(($locale) => {
    document.documentElement.lang = $locale;
  });
}
