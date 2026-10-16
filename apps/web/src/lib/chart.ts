/**
 * Mise en forme des séries temporelles pour le graphique.
 *
 * Règle centrale : un bucket sans mesure conserve un trou. On n'interpole
 * jamais entre deux points et on ne remplace jamais une absence par 0.
 *
 * Attention au piège : Recharts ne connaît que les points qu'on lui donne. Si
 * l'API ne renvoie pas un bucket (parce qu'il est vide), deux buckets voisins
 * deviennent adjacents dans le tableau et la courbe est **tracée en travers du
 * trou** — c'est précisément l'interpolation que l'on veut éviter. Il faut donc
 * matérialiser les buckets manquants par des points `null`, que
 * `connectNulls={false}` transforme en coupure visible.
 */
import type { Bucket } from './schema'

export type PowerPoint = {
  /** Horodatage du début de bucket, en millisecondes (axe X). */
  t: number
  /** Libellé lisible du bucket. */
  label: string
  /** Puissance moyenne du bucket, ou `null` si aucune mesure n'y existe. */
  powerW: number | null
  /** Nombre d'échantillons agrégés (0 pour un bucket manquant). */
  count: number
  /** Nombre d'échantillons du bucket sans valeur de puissance. */
  missing: number
}

/** Fenêtre demandée à l'API, utilisée pour compléter les buckets manquants. */
export type TimeWindow = {
  from: string
  to: string
}

/**
 * Nombre maximal de points pour lesquels la grille est complétée.
 *
 * `step=auto` borne déjà la réponse à 1500 points ; cette limite protège le cas
 * d'une plage personnalisée avec un pas fin sur une longue durée, où compléter
 * la grille ferait exploser le nombre de points rendus. Au-delà, seuls les
 * buckets réellement renvoyés sont affichés.
 */
export const MAX_FILLED_POINTS = 5000

const timeFormat = new Intl.DateTimeFormat('fr-BE', { hour: '2-digit', minute: '2-digit' })
const dayTimeFormat = new Intl.DateTimeFormat('fr-BE', {
  day: '2-digit',
  month: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
})
const dateFormat = new Intl.DateTimeFormat('fr-BE', { day: '2-digit', month: '2-digit' })

function labelFor(date: Date, stepSeconds: number): string {
  if (stepSeconds >= 86400) {
    return dateFormat.format(date)
  }
  if (stepSeconds >= 3600) {
    return dayTimeFormat.format(date)
  }
  return timeFormat.format(date)
}

function pointFromBucket(bucket: Bucket, stepSeconds: number): PowerPoint {
  const date = new Date(bucket.start)
  return {
    t: date.getTime(),
    label: labelFor(date, stepSeconds),
    powerW: bucket.power_pv_avg_w,
    count: bucket.count,
    missing: bucket.missing,
  }
}

/** Point sans mesure : matérialise un bucket absent de la réponse. */
function blankPoint(t: number, stepSeconds: number): PowerPoint {
  return { t, label: labelFor(new Date(t), stepSeconds), powerW: null, count: 0, missing: 0 }
}

function mapBuckets(buckets: Bucket[], stepSeconds: number): PowerPoint[] {
  return buckets.map((bucket) => pointFromBucket(bucket, stepSeconds))
}

/**
 * Convertit des buckets d'API en points de graphique.
 *
 * @param buckets buckets non vides renvoyés par l'API
 * @param stepSeconds pas du bucket (déduit par `inferStepSeconds`)
 * @param window fenêtre demandée ; fournie, elle fait apparaître les buckets
 *   manquants sous forme de points sans mesure
 */
export function toPowerPoints(buckets: Bucket[], stepSeconds = 300, window?: TimeWindow): PowerPoint[] {
  if (!window || stepSeconds <= 0) {
    return mapBuckets(buckets, stepSeconds)
  }

  const fromMs = Date.parse(window.from)
  const toMs = Date.parse(window.to)
  const stepMs = stepSeconds * 1000
  if (!Number.isFinite(fromMs) || !Number.isFinite(toMs) || toMs <= fromMs) {
    return mapBuckets(buckets, stepSeconds)
  }

  // Les buckets sont alignés sur l'epoch côté serveur : la grille doit l'être aussi.
  const firstMs = Math.ceil(fromMs / stepMs) * stepMs
  const expected = Math.ceil((toMs - firstMs) / stepMs)
  if (expected <= 0 || expected > MAX_FILLED_POINTS) {
    return mapBuckets(buckets, stepSeconds)
  }

  const known = new Map<number, Bucket>()
  for (const bucket of buckets) {
    known.set(Date.parse(bucket.start), bucket)
  }

  const points: PowerPoint[] = []
  for (let t = firstMs; t < toMs; t += stepMs) {
    const bucket = known.get(t)
    points.push(bucket ? pointFromBucket(bucket, stepSeconds) : blankPoint(t, stepSeconds))
  }
  return points
}

/**
 * Déduit le pas réel d'une série à partir de ses deux premiers buckets.
 *
 * L'API ne renvoie pas le pas résolu pour `step=auto` ; l'écart entre deux
 * buckets consécutifs le donne. Le repli sert quand la série ne contient qu'un
 * point — le format des libellés est alors sans importance.
 */
export function inferStepSeconds(buckets: Bucket[], fallbackSeconds = 300): number {
  const [first, second] = buckets
  if (!first || !second) {
    return fallbackSeconds
  }
  const delta = (new Date(second.start).getTime() - new Date(first.start).getTime()) / 1000
  return delta > 0 ? delta : fallbackSeconds
}

export type SeriesSummary = {
  /** Buckets contenant au moins une mesure. */
  measured: number
  /** Buckets sans aucune mesure de puissance : les trous de la courbe. */
  withoutMeasure: number
  /** Échantillons agrégés, toutes valeurs confondues. */
  samples: number
  /** Échantillons dépourvus de valeur de puissance. */
  missingSamples: number
  minW: number | null
  maxW: number | null
  avgW: number | null
}

/**
 * Résumé textuel d'une série : sert de description accessible au graphique et
 * de garde-fou contre un écran vide inexplicable.
 */
export function summarize(points: PowerPoint[]): SeriesSummary {
  let measured = 0
  let withoutMeasure = 0
  let samples = 0
  let missingSamples = 0
  let minW: number | null = null
  let maxW: number | null = null
  let sum = 0

  for (const point of points) {
    samples += point.count
    missingSamples += point.missing
    if (point.powerW === null) {
      withoutMeasure += 1
      continue
    }
    measured += 1
    sum += point.powerW
    minW = minW === null ? point.powerW : Math.min(minW, point.powerW)
    maxW = maxW === null ? point.powerW : Math.max(maxW, point.powerW)
  }

  return {
    measured,
    withoutMeasure,
    samples,
    missingSamples,
    minW,
    maxW,
    avgW: measured > 0 ? sum / measured : null,
  }
}
