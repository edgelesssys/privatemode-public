import { writable, readable } from 'svelte/store';

export type ThemePreference = 'system' | 'light' | 'dark';

const STORAGE_KEY = 'privatemode_theme';
const LIGHT_THEME_COLOR = '#FFFFFF';
const DARK_THEME_COLOR = '#18181B';

function applyBrowserTheme(resolvedDark: boolean) {
  const theme = resolvedDark ? 'dark' : 'light';
  const themeColor = resolvedDark ? DARK_THEME_COLOR : LIGHT_THEME_COLOR;

  document.documentElement.setAttribute('data-theme', theme);
  document.documentElement.style.colorScheme = theme;

  const themeColorMeta = document.querySelector(
    'meta[name="theme-color"]',
  ) as HTMLMetaElement;
  themeColorMeta.setAttribute('content', themeColor);
}

function getStoredPreference(): ThemePreference {
  if (typeof localStorage === 'undefined') return 'system';
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'light' || stored === 'dark' || stored === 'system')
    return stored;
  return 'system';
}

export const themePreference = writable<ThemePreference>(getStoredPreference());

export const isDark = readable<boolean>(false, (set) => {
  if (typeof window === 'undefined') return;

  const mql = window.matchMedia('(prefers-color-scheme: dark)');

  function resolve(pref: ThemePreference) {
    set(pref === 'system' ? mql.matches : pref === 'dark');
  }

  const unsubscribe = themePreference.subscribe(resolve);
  const handler = () => {
    const pref = getStoredPreference();
    if (pref === 'system') resolve('system');
  };
  mql.addEventListener('change', handler);

  return () => {
    unsubscribe();
    mql.removeEventListener('change', handler);
  };
});

export function initTheme(): () => void {
  const mql = window.matchMedia('(prefers-color-scheme: dark)');

  function applyTheme(pref: ThemePreference) {
    let resolvedDark: boolean;
    if (pref === 'system') {
      resolvedDark = mql.matches;
    } else {
      resolvedDark = pref === 'dark';
    }
    applyBrowserTheme(resolvedDark);
  }

  const unsubscribe = themePreference.subscribe((pref) => {
    localStorage.setItem(STORAGE_KEY, pref);
    applyTheme(pref);
  });

  function handleSystemChange() {
    const pref = getStoredPreference();
    if (pref === 'system') {
      applyTheme('system');
    }
  }

  mql.addEventListener('change', handleSystemChange);

  return () => {
    unsubscribe();
    mql.removeEventListener('change', handleSystemChange);
  };
}
