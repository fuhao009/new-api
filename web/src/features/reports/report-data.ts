import type { QuotaDataItem } from '@/features/dashboard/types'

export function reportPeriod(date: string, mode: 'week' | 'month') {
  const selected = new Date(`${date}T00:00:00`)
  if (Number.isNaN(selected.getTime())) throw new Error('Invalid date')
  const start = new Date(selected)
  if (mode === 'week')
    {start.setDate(start.getDate() - ((start.getDay() + 6) % 7))}
  else start.setDate(1)
  const end = new Date(start)
  const previousStart = new Date(start)
  if (mode === 'week') {
    end.setDate(end.getDate() + 7)
    previousStart.setDate(previousStart.getDate() - 7)
  } else {
    end.setMonth(end.getMonth() + 1)
    previousStart.setMonth(previousStart.getMonth() - 1)
  }
  return {
    start: Math.floor(start.getTime() / 1000),
    end: Math.floor(end.getTime() / 1000) - 1,
    previousStart: Math.floor(previousStart.getTime() / 1000),
    previousEnd: Math.floor(start.getTime() / 1000) - 1,
  }
}

export function aggregateReport(rows: QuotaDataItem[]) {
  const models = new Map<
    string,
    { name: string; requests: number; tokens: number; quota: number }
  >()
  const days = new Map<
    string,
    { name: string; requests: number; tokens: number; quota: number }
  >()
  let requests = 0,
    tokens = 0,
    quota = 0
  for (const row of rows) {
    const count = row.count ?? 0,
      used = row.token_used ?? 0,
      cost = row.quota ?? 0
    requests += count
    tokens += used
    quota += cost
    const date = new Date(row.created_at * 1000)
    const day = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
    for (const [map, key] of [
      [models, row.model_name ?? 'Unknown'],
      [days, day],
    ] as const) {
      const entry = map.get(key) ?? {
        name: key,
        requests: 0,
        tokens: 0,
        quota: 0,
      }
      entry.requests += count
      entry.tokens += used
      entry.quota += cost
      map.set(key, entry)
    }
  }
  return {
    requests,
    tokens,
    quota,
    models: [...models.values()].sort((a, b) => b.quota - a.quota),
    days: [...days.values()].sort((a, b) => a.name.localeCompare(b.name)),
  }
}
