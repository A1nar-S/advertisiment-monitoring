import { ref } from 'vue'

// Light, dark or auto (follows the system). The resolved theme is set as
// <html data-theme>, which style.css keys the dark tokens on. index.html
// applies the saved choice before the app loads, so there's no flash.

export type ThemeMode = 'light' | 'dark' | 'auto'
const storageKey = 'theme'
const system = window.matchMedia('(prefers-color-scheme: dark)')

function saved(): ThemeMode {
  try {
    const value = localStorage.getItem(storageKey)
    if (value === 'light' || value === 'dark' || value === 'auto') return value
  } catch {
    // Storage blocked: follow the system.
  }
  return 'auto'
}

export const themeMode = ref<ThemeMode>(saved())

function apply() {
  const dark =
    themeMode.value === 'dark' || (themeMode.value === 'auto' && system.matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute('content', dark ? '#0d0d0d' : '#f6f7f9')
}

export function setThemeMode(mode: ThemeMode) {
  themeMode.value = mode
  try {
    localStorage.setItem(storageKey, mode)
  } catch {
    // Applied for this visit only.
  }
  apply()
}

system.addEventListener('change', apply)
apply()
