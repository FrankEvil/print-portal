export type ThemeMode = 'auto' | 'light' | 'dark'

const KEY = 'print_portal_theme'

export function getTheme(): ThemeMode {
  const v = localStorage.getItem(KEY)
  return v === 'light' || v === 'dark' ? v : 'auto'
}

export function applyTheme(mode: ThemeMode) {
  localStorage.setItem(KEY, mode)
  const root = document.documentElement
  if (mode === 'auto') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', mode)
}

applyTheme(getTheme())
