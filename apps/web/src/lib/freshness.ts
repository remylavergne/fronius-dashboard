/**
 * Décision de péremption affichée à l'utilisateur.
 *
 * En marche nominale, c'est le serveur qui tranche (`meta.stale`), car lui seul
 * connaît son seuil et son horloge. Mais quand l'API est injoignable, elle ne
 * peut plus rien dire : conserver son dernier verdict afficherait « Données à
 * jour » sur des données qui ne le sont plus depuis longtemps. On applique alors
 * la même règle localement — âge de la dernière mesure comparé au seuil.
 */
import type { Meta } from './schema'

/** Seuil de repli si l'API n'a pas communiqué le sien. */
const FALLBACK_STALE_AFTER_S = 30

export function resolveStale(meta: Meta | undefined, apiUnreachable: boolean, now: number = Date.now()): boolean | undefined {
  if (!apiUnreachable) {
    return meta?.stale
  }

  const sampledAt = meta?.sampled_at
  if (!sampledAt) {
    // Aucun instant de référence : impossible d'affirmer quoi que ce soit, mais
    // l'absence de réponse de l'API est déjà signalée par le bandeau.
    return undefined
  }
  const sampledMs = Date.parse(sampledAt)
  if (Number.isNaN(sampledMs)) {
    return true
  }
  const thresholdMs = (meta?.stale_after_s ?? FALLBACK_STALE_AFTER_S) * 1000
  return now - sampledMs > thresholdMs
}
