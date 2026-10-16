/**
 * Mise en forme des valeurs pour l'affichage.
 *
 * L'API expose des unités SI de base (W, Wh, %) ; la conversion en kW et kWh
 * est faite ici, à la frontière d'affichage, jamais dans le modèle ni en base.
 * Locale : fr-BE (virgule décimale, espace insécable comme séparateur de
 * milliers).
 */

const decimal0 = new Intl.NumberFormat('fr-BE', { maximumFractionDigits: 0 })
const decimal1 = new Intl.NumberFormat('fr-BE', { maximumFractionDigits: 1 })
const decimal2 = new Intl.NumberFormat('fr-BE', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** Valeur formatée plus son unité, pour éviter toute ambiguïté d'échelle. */
export type Formatted = { value: string; unit: string }

/**
 * Puissance : kW au-delà de 1 kW, sinon W.
 *
 * Une valeur absente (`null` ou `undefined`) n'est jamais rendue « 0 » :
 * l'appelant reçoit un tiret, ce qui préserve l'invariant « pas de zéro
 * inventé » jusqu'à l'écran.
 */
export function formatW(watts: number | null | undefined): Formatted {
  if (watts === null || watts === undefined || !Number.isFinite(watts)) {
    return { value: '—', unit: '' }
  }
  if (Math.abs(watts) >= 1000) {
    return { value: decimal2.format(watts / 1000), unit: 'kW' }
  }
  return { value: decimal0.format(watts), unit: 'W' }
}

/** Énergie : kWh au-delà de 1 kWh, sinon Wh. */
export function formatKWh(wattHours: number | null | undefined): Formatted {
  if (wattHours === null || wattHours === undefined || !Number.isFinite(wattHours)) {
    return { value: '—', unit: '' }
  }
  if (Math.abs(wattHours) >= 1000) {
    return { value: decimal2.format(wattHours / 1000), unit: 'kWh' }
  }
  return { value: decimal0.format(wattHours), unit: 'Wh' }
}

/** Pourcentage. */
export function formatPct(pct: number | null | undefined): string {
  if (pct === null || pct === undefined || !Number.isFinite(pct)) {
    return '—'
  }
  return `${decimal1.format(pct)} %`
}

/**
 * Âge d'un horodatage, en français.
 *
 * Une chaîne vide (dernier succès jamais enregistré) ou illisible donne « — ».
 */
export function formatAge(isoTimestamp: string | null | undefined, now: Date = new Date()): string {
  if (!isoTimestamp) {
    return '—'
  }
  const then = new Date(isoTimestamp)
  const time = then.getTime()
  if (Number.isNaN(time)) {
    return '—'
  }

  const age = now.getTime() - time
  if (age < 0) {
    // Horloge locale en retard sur l'appareil : ne pas afficher d'âge négatif.
    return 'à l’instant'
  }
  if (age < 5 * SECOND) {
    return 'à l’instant'
  }
  if (age < MINUTE) {
    return `il y a ${Math.round(age / SECOND)} s`
  }
  if (age < HOUR) {
    return `il y a ${Math.round(age / MINUTE)} min`
  }
  if (age < DAY) {
    return `il y a ${Math.round(age / HOUR)} h`
  }
  return `il y a ${Math.round(age / DAY)} j`
}

/** Horodatage local complet, pour les infobulles et les pages de détail. */
export function formatDateTime(isoTimestamp: string | null | undefined): string {
  if (!isoTimestamp) {
    return '—'
  }
  const date = new Date(isoTimestamp)
  if (Number.isNaN(date.getTime())) {
    return '—'
  }
  return new Intl.DateTimeFormat('fr-BE', {
    dateStyle: 'short',
    timeStyle: 'medium',
  }).format(date)
}

/** Durée en millisecondes, en français court. */
export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms)) {
    return '—'
  }
  if (ms < 1000) {
    return `${decimal0.format(ms)} ms`
  }
  return `${decimal2.format(ms / 1000)} s`
}
