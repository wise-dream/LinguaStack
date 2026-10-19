export interface GlossaryRow {
  source: string
  target: string
}

/** Глоссарий для API: строки без термина или перевода отбрасываются. */
export function glossaryFromRows(rows: GlossaryRow[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const source = row.source.trim()
    const target = row.target.trim()
    if (source && target) out[source] = target
  }
  return out
}
