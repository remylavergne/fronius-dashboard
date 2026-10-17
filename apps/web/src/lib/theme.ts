/**
 * Light / dark / system theme.
 *
 * The model is deliberately small: the stored preference is one of three
 * modes, the *resolved* theme is the explicit mode or the OS preference, and
 * applying it is three side effects (`html.dark`, `color-scheme`, the
 * `theme-color` meta). The no-flash counterpart of `applyTheme` is the inline
 * script in `index.html`: keep the two in step.
 *
 * This file holds the pure logic, the context and the hook; the provider
 * component lives in `theme.tsx` so that each file exports one kind of thing.
 */
import { createContext, useContext } from 'react'

export type ThemeMode = 'system' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

export const THEME_STORAGE_KEY = 'theme'
export const DARK_MEDIA_QUERY = '(prefers-color-scheme: dark)'

/** `theme-color` values, matching `--background` in `index.css`. */
export const THEME_COLORS: Record<ResolvedTheme, string> = {
  light: '#ffffff',
  dark: '#171717',
}

const THEME_MODES: readonly ThemeMode[] = ['system', 'light', 'dark']

export function isThemeMode(value: string | null | undefined): value is ThemeMode {
  return value !== null && value !== undefined && (THEME_MODES as readonly string[]).includes(value)
}

/** Stored preference; a missing, unreadable or invalid value means `system`. */
export function readStoredTheme(): ThemeMode {
  if (typeof window === 'undefined') {
    return 'system'
  }
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
    return isThemeMode(stored) ? stored : 'system'
  } catch {
    return 'system'
  }
}

/** OS preference. jsdom has no `matchMedia`, so its absence reads as light. */
export function systemPrefersDark(): boolean {
  return darkMediaQuery()?.matches ?? false
}

export function darkMediaQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return null
  }
  return window.matchMedia(DARK_MEDIA_QUERY)
}

export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  if (mode === 'system') {
    return prefersDark ? 'dark' : 'light'
  }
  return mode
}

/**
 * Applies a resolved theme to the document: the class drives the tokens,
 * `color-scheme` makes native controls and scrollbars follow, and the meta
 * keeps the browser chrome in step.
 */
export function applyTheme(theme: ResolvedTheme): void {
  if (typeof document === 'undefined') {
    return
  }
  const root = document.documentElement
  root.classList.toggle('dark', theme === 'dark')
  root.style.colorScheme = theme
  document
    .querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    ?.setAttribute('content', THEME_COLORS[theme])
}

export type ThemeContextValue = {
  /** The chosen mode, persisted. */
  mode: ThemeMode
  /** The mode actually painted, the OS preference folded in. */
  resolved: ResolvedTheme
  setMode: (mode: ThemeMode) => void
  /** `system → light → dark → system`. */
  cycleMode: () => void
}

export const ThemeContext = createContext<ThemeContextValue | null>(null)

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return context
}
