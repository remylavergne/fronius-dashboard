/**
 * Accès aux traductions.
 *
 * La phase 1 ne sert qu'une langue. `useT()` lit le dictionnaire de la locale
 * courante et retourne une fonction de traduction typée sur les clés de `fr`.
 * Ajouter une langue revient à fournir un second dictionnaire et à rendre la
 * locale réactive (contexte) — aucun composant n'a à changer.
 */
import { fr } from './fr'
import type { Dictionary, TranslationKey } from './fr'

export type Locale = 'fr'

/** Langues disponibles, prêtes pour l'i18n de la phase 4. */
export const dictionaries: Record<Locale, Dictionary> = { fr }

const currentLocale: Locale = 'fr'

export type Translator = {
  locale: Locale
  /** Retourne la traduction, ou la clé elle-même si elle manque (rend l'écart visible). */
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
