/** Value of an export parameter: an absent one is left out of the query. */
type ExportValue = string | number | undefined

/**
 * Download URL of one history route: the exact parameters the page fetched
 * with, plus `format=csv`.
 *
 * The pages render this as a plain `<a href download>` and never as a fetch:
 * the browser performs the request, names the file from `Content-Disposition`
 * and streams it to disk without the bytes passing through React state.
 */
export function csvDownloadUrl(path: string, params: Record<string, ExportValue>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined) {
      search.set(key, String(value))
    }
  }
  search.set('format', 'csv')
  return `${path}?${search.toString()}`
}
