import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  ThemeContext,
  applyTheme,
  darkMediaQuery,
  isThemeMode,
  readStoredTheme,
  resolveTheme,
  systemPrefersDark,
  THEME_STORAGE_KEY,
} from './theme'
import type { ThemeContextValue, ThemeMode } from './theme'

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(readStoredTheme)
  const [systemDark, setSystemDark] = useState(systemPrefersDark)

  // Live OS preference: only affects `system`, but listening unconditionally
  // means the switch is immediate when the user goes back to `system`. The
  // initial state already reads the current value, so no resync is needed.
  useEffect(() => {
    const query = darkMediaQuery()
    if (!query) {
      return
    }
    const onChange = (event: MediaQueryListEvent) => setSystemDark(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  // Another tab changed the preference. `key === null` is a `clear()`.
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key !== null && event.key !== THEME_STORAGE_KEY) {
        return
      }
      setModeState(isThemeMode(event.newValue) ? event.newValue : 'system')
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  const resolved = resolveTheme(mode, systemDark)

  useEffect(() => {
    applyTheme(resolved)
  }, [resolved])

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next)
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // Storage can be unavailable (private mode, quota): the session still
      // follows the choice, only persistence is lost.
    }
  }, [])

  const cycleMode = useCallback(() => {
    setMode(mode === 'system' ? 'light' : mode === 'light' ? 'dark' : 'system')
  }, [mode, setMode])

  const value = useMemo<ThemeContextValue>(
    () => ({ mode, resolved, setMode, cycleMode }),
    [mode, resolved, setMode, cycleMode],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
