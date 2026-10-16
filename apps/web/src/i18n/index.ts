/**
 * Translation access.
 *
 * Phase 1 serves a single language. `useT()` reads the current locale's
 * dictionary and returns a translation function typed on the keys of `en`.
 * Adding a language means supplying a second dictionary and making the locale
 * reactive (context) — no component has to change.
 */
import { en } from './en'
import type { Dictionary, TranslationKey } from './en'

export type Locale = 'en'

/** Available languages, ready for phase 4 i18n. */
export const dictionaries: Record<Locale, Dictionary> = { en }

const currentLocale: Locale = 'en'

export type Translator = {
  locale: Locale
  /** Returns the translation, or the key itself when missing (makes the gap visible). */
  t: (key: TranslationKey) => string
}

export function useT(): Translator {
  const dictionary = dictionaries[currentLocale]
  return {
    locale: currentLocale,
    t: (key: TranslationKey) => dictionary[key] ?? key,
  }
}

export type { TranslationKey }
