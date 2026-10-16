/**
 * Energies: choosing what an energy display shows.
 *
 * Two levels live here. The KPI grid of the overview resolves a *sample*: the
 * firmware of the reference installation publishes neither the day nor the year
 * counter on any local endpoint (`docs/UNKNOWNS.md` #13), so the API carries
 * them as `null` and, alongside, an `energy_derived` object computed from our own
 * stored total. The measured counter wins whenever it exists; otherwise the
 * derived one is used **and flagged**, so a computed value never looks like a
 * measurement. With neither available, the value stays `null` and the display
 * shows its dash — never a zero.
 *
 * The daily page resolves the same way, one *local day* at a time
 * (`/api/v1/energy/daily`), and adds what its page needs: the chart points, with
 * the days nothing was recorded on kept as gaps, and the three rollups.
 *
 * Lives here, outside the route module, so the pages keep exporting components
 * only (react-refresh).
 */
import type { DailyEnergy } from './schema'

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

// ---------------------------------------------------------------------------
// One local day (/api/v1/energy/daily)
// ---------------------------------------------------------------------------

/** Which counter a day's value comes from: nothing is displayed without saying so. */
export type EnergySource = 'measured' | 'derived'

export type ResolvedDay = {
  /** Energy of the day, `null` when no counter carries one. */
  wh: number | null
  /** `measured` for the device's own counter, `derived` for ours, `null` for a dash. */
  source: EnergySource | null
  /** Sentence of the backend explaining an absence; `''` when it has nothing to say. */
  reason: string
}

/**
 * Resolves one day of the daily energy.
 *
 * The device's own counter (`energy_wh`) wins when it exists; otherwise the
 * value derived from our stored total (`derived_wh`) is used **and marked** —
 * it is measured, but it is not what the device reported. With neither, the day
 * is a dash, and the backend's `derived_reason` travels with it: a dash without
 * its reason leaves the reader to guess whether the application is broken.
 */
export function resolveDayEnergy(day: DailyEnergy): ResolvedDay {
  // The reason explains why the DERIVED value is absent: it belongs to the dash
  // and to nothing else. A day the device accounted for carries a value, and
  // printing the derivation's excuse beside it would read as a defect.
  const measured = day.energy_wh
  if (measured !== null && measured !== undefined) {
    return { wh: measured, source: 'measured', reason: '' }
  }
  const derived = day.derived_wh
  if (derived !== null && derived !== undefined) {
    return { wh: derived, source: 'derived', reason: '' }
  }
  return { wh: null, source: null, reason: day.derived_reason ?? '' }
}

/** One point of the daily energy chart. */
export type DailyPoint = ResolvedDay & {
  /** Local day, `YYYY-MM-DD`, as the backend named it. */
  date: string
  /** Short axis label (`16/10`), derived from the string — never from `new Date(date)`. */
  label: string
  /** Day in progress: its value grows until the day ends. */
  partial: boolean
}

const pad2 = (value: number) => String(value).padStart(2, '0')

/**
 * A local day (`YYYY-MM-DD`) as a UTC-noon `Date`.
 *
 * Noon, and `Date.UTC`: this is pure calendar arithmetic — stepping one day is
 * one day, whatever the time zone and whatever the DST. The `date` of the
 * payload is a **local** day of the installation, so it must never be parsed by
 * `new Date('2026-10-16')`, which reads it as UTC midnight and shifts it by a
 * day west of Greenwich.
 */
function dayToDate(date: string): Date {
  const [year, month, day] = date.split('-').map((part) => Number.parseInt(part, 10))
  return new Date(Date.UTC(year ?? 0, (month ?? 1) - 1, day ?? 1, 12))
}

/** Local day of a UTC-noon `Date`, `YYYY-MM-DD` — the reverse of `dayToDate`. */
function dateToDay(date: Date): string {
  return `${String(date.getUTCFullYear())}-${pad2(date.getUTCMonth() + 1)}-${pad2(date.getUTCDate())}`
}

/** First day of the week (Monday) of a UTC-noon day. */
function startOfWeek(day: Date): Date {
  const offset = (day.getUTCDay() + 6) % 7
  return new Date(Date.UTC(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate() - offset, 12))
}

/**
 * Every local day from `start` to `end`, both included.
 *
 * The single enumerator of the page: the chart's grid and the rollups' sums must
 * walk the same days, or a gap would be counted twice in one place and lost in
 * the other.
 */
function daysBetween(start: Date, end: Date): string[] {
  const days: string[] = []
  for (let cursor = start; cursor.getTime() <= end.getTime(); cursor.setUTCDate(cursor.getUTCDate() + 1)) {
    days.push(dateToDay(cursor))
  }
  return days
}

/**
 * Converts the daily energies into chart points, materialising every day of the
 * returned range.
 *
 * Central rule, identical to the power curve: a day without a value keeps a
 * **gap**. Recharts only knows the points it is given, so a day the payload does
 * not carry would make its two neighbours adjacent and the chart would hide the
 * hole; every day between the first and the last returned day therefore becomes
 * a point, with `wh: null` when nothing can be resolved — no bar is drawn there.
 * Nothing is ever interpolated, and an absence is never a zero.
 *
 * Days outside the span the backend returned carry no point: nothing is known
 * about them, and that span is the spine of the answer (`dailyToDTO`).
 */
export function toDailyPoints(days: DailyEnergy[]): DailyPoint[] {
  const sorted = [...days].sort((left, right) => (left.date < right.date ? -1 : left.date > right.date ? 1 : 0))
  const first = sorted[0]
  const last = sorted[sorted.length - 1]
  if (!first || !last) {
    return []
  }

  const known = new Map(sorted.map((day) => [day.date, day]))
  return daysBetween(dayToDate(first.date), dayToDate(last.date)).map((date) => {
    const day = known.get(date)
    const resolved = day ? resolveDayEnergy(day) : { wh: null, source: null, reason: '' }
    const [, month, dayOfMonth] = date.split('-')
    return {
      ...resolved,
      date,
      label: month !== undefined && dayOfMonth !== undefined ? `${dayOfMonth}/${month}` : date,
      partial: day?.partial === true,
    }
  })
}

/** The three rollups of the energy page, in display order. */
export const ROLLUP_PERIODS = ['week', 'month', 'year'] as const
export type RollupPeriod = (typeof ROLLUP_PERIODS)[number]

export type Rollup = {
  period: RollupPeriod
  /** Sum of the resolved days, `null` when not a single day of the period had a value. */
  wh: number | null
  /** Days of the period that carried a value and went into the sum. */
  contributed: number
  /** Elapsed days of the period that carried none: they are missing from the sum. */
  excluded: number
  /** Elapsed days of the period — `contributed + excluded`. */
  days: number
}

/**
 * Sums the resolved days over this week, this month and this year.
 *
 * The sum is taken over the days the payload holds: a day the backend published
 * without a value contributes nothing and is **counted as excluded**, so the
 * reader always knows how many days the total rests on. The three periods all
 * end today, which is the day in progress — today's energy grows until midnight —
 * so every total here is a floor, and the page says so.
 *
 * The calendar used is the browser's: the days themselves are the installation's
 * local days, and a day's month and year are read from its own string; the only
 * boundary that can differ is the week's, by one day, if the installation and
 * the reader sit on opposite sides of midnight.
 */
export function summarizeRollups(days: DailyEnergy[], now: Date): Rollup[] {
  const byDate = new Map(days.map((day) => [day.date, day]))
  const today = dayToDate(`${String(now.getFullYear())}-${pad2(now.getMonth() + 1)}-${pad2(now.getDate())}`)

  return ROLLUP_PERIODS.map((period) => {
    const elapsedDays =
      period === 'week'
        ? daysBetween(startOfWeek(today), today)
        : period === 'month'
          ? daysBetween(new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), 1, 12)), today)
          : daysBetween(new Date(Date.UTC(today.getUTCFullYear(), 0, 1, 12)), today)

    let contributed = 0
    let wh = 0
    for (const date of elapsedDays) {
      const day = byDate.get(date)
      const value = day ? resolveDayEnergy(day).wh : null
      if (value === null) {
        continue
      }
      contributed += 1
      wh += value
    }

    return {
      period,
      wh: contributed === 0 ? null : wh,
      contributed,
      excluded: elapsedDays.length - contributed,
      days: elapsedDays.length,
    }
  })
}
