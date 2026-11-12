import { describe, expect, it } from 'vitest'

import { aggregateReport, reportPeriod } from '../report-data'

describe('report calendar boundaries', () => {
  it('uses Monday through Sunday and the immediately preceding week', () => {
    const period = reportPeriod('2026-11-11', 'week')
    expect(new Date(period.start * 1000).getDay()).toBe(1)
    expect(new Date(period.end * 1000).getDay()).toBe(0)
    expect(period.previousEnd + 1).toBe(period.start)
    expect(new Date(period.start * 1000).getDate()).toBe(9)
  })
  it('compares complete calendar months across leap February', () => {
    const period = reportPeriod('2024-03-15', 'month')
    expect(new Date(period.previousStart * 1000).getMonth()).toBe(1)
    expect(new Date(period.previousEnd * 1000).getDate()).toBe(29)
    expect(new Date(period.end * 1000).getDate()).toBe(31)
  })
  it('aggregates each model without losing request or token totals', () => {
    const time = new Date(2026, 10, 9).getTime() / 1000
    const result = aggregateReport([
      {
        created_at: time,
        model_name: 'alpha',
        count: 2,
        token_used: 10,
        quota: 20,
      },
      {
        created_at: time,
        model_name: 'beta',
        count: 1,
        token_used: 30,
        quota: 50,
      },
      {
        created_at: time + 86400,
        model_name: 'alpha',
        count: 3,
        token_used: 40,
        quota: 10,
      },
    ])
    expect({
      requests: result.requests,
      tokens: result.tokens,
      quota: result.quota,
    }).toEqual({ requests: 6, tokens: 80, quota: 80 })
    expect(result.models).toEqual([
      { name: 'beta', requests: 1, tokens: 30, quota: 50 },
      { name: 'alpha', requests: 5, tokens: 50, quota: 30 },
    ])
    expect(result.days.map((row) => row.quota)).toEqual([70, 10])
  })
})
