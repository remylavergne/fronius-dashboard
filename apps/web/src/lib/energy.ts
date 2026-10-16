/**
 * Choosing the value an energy KPI displays.
 *
 * The firmware of the reference installation publishes neither the day nor the
 * year counter on any local endpoint (`docs/UNKNOWNS.md` #13): the API carries
 * them as `null` and, alongside, an `energy_derived` object computed from our own
 * stored total. The measured counter wins whenever it exists; otherwise the
 * derived one is used **and flagged**, so a computed value never looks like a
 * measurement. With neither available, the value stays `null` and the card shows
 * its dash — never a zero.
 *
 * Lives here, outside the route module, so the page keeps exporting components
 * only (react-refresh).
 */

export type ResolvedEnergy = { wh: number | null; estimated: boolean }

/** Picks the value an energy KPI must display. */
export const resolveEnergy = (
  measured: number | null | undefined,
  derived: number | null | undefined,
): ResolvedEnergy => {
  if (measured !== null && measured !== undefined) return { wh: measured, estimated: false }
  if (derived !== null && derived !== undefined) return { wh: derived, estimated: true }
  return { wh: null, estimated: false }
}
