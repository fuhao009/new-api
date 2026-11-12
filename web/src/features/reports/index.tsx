import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { getUserQuotaDates, getFlowQuotaDates } from '@/features/dashboard/api'
import { ConsumptionDistributionChart } from '@/features/dashboard/components/models/consumption-distribution-chart'
import { getUserProfile } from '@/features/profile/api'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { aggregateReport, reportPeriod } from './report-data'

export function UsageReports() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [mode, setMode] = useState<'week' | 'month'>('week')
  const [date, setDate] = useState(() => {
    const d = new Date()
    d.setDate(d.getDate() - 7)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  })
  const period = useMemo(() => reportPeriod(date, mode), [date, mode])
  const report = useQuery({
    queryKey: ['usage-report', userId, period],
    queryFn: async () => {
      const [current, previous, flow, profile] = await Promise.all([
        getUserQuotaDates(
          { start_timestamp: period.start, end_timestamp: period.end },
          false
        ),
        getUserQuotaDates(
          {
            start_timestamp: period.previousStart,
            end_timestamp: period.previousEnd,
          },
          false
        ),
        getFlowQuotaDates(
          { start_timestamp: period.start, end_timestamp: period.end },
          false
        ),
        getUserProfile(),
      ])
      for (const response of [current, previous, flow, profile])
        {requireServerSuccess(response)}
      const tokenMap = new Map<
        number,
        {
          id: number
          name: string
          requests: number
          tokens: number
          quota: number
        }
      >()
      for (const row of flow.data ?? []) {
        const id = row.token_id ?? 0
        const entry = tokenMap.get(id) ?? {
          id,
          name: row.token_name ?? t('Deleted token'),
          requests: 0,
          tokens: 0,
          quota: 0,
        }
        entry.requests += row.count ?? 0
        entry.tokens += row.token_used ?? 0
        entry.quota += row.quota ?? 0
        tokenMap.set(id, entry)
      }
      return {
        current: aggregateReport(current.data ?? []),
        previous: aggregateReport(previous.data ?? []),
        raw: current.data ?? [],
        tokens: [...tokenMap.values()].sort((a, b) => b.quota - a.quota),
        profile: profile.data,
      }
    },
  })
  const data = report.data
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Usage reports')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <NativeSelect
          aria-label={t('Report period')}
          value={mode}
          onChange={(event) => setMode(event.target.value as 'week' | 'month')}
        >
          <NativeSelectOption value='week'>{t('Weekly')}</NativeSelectOption>
          <NativeSelectOption value='month'>{t('Monthly')}</NativeSelectOption>
        </NativeSelect>
        <Input
          aria-label={t('Report date')}
          type='date'
          value={date}
          onChange={(event) => {
            if (event.target.value) setDate(event.target.value)
          }}
        />
        <Button
          disabled={!data || report.isFetching}
          onClick={() => window.print()}
        >
          {t('Print / Save PDF')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <article className='usage-report space-y-6'>
          <h1 className='text-2xl font-semibold'>{t('API usage report')}</h1>
          <p>
            {new Date(period.start * 1000).toLocaleDateString()} —{' '}
            {new Date(period.end * 1000).toLocaleDateString()}
          </p>
          {report.isPending && <p>{t('Loading...')}</p>}
          {report.isError && (
            <p role='alert'>
              {t(
                'Failed to load report. The selected period may exceed the retention policy.'
              )}
            </p>
          )}
          {data && (
            <>
              <p>{data.profile?.username}</p>
              <div className='grid gap-4 sm:grid-cols-3'>
                {(['requests', 'tokens', 'quota'] as const).map((metric) => {
                  const current = data.current[metric],
                    previous = data.previous[metric]
                  let comparison = t('No previous usage')
                  if (previous > 0)
                    {comparison = `${(((current - previous) / previous) * 100).toFixed(1)}%`}
                  if (previous === 0 && current === 0) comparison = '0%'
                  return (
                    <section key={metric} className='rounded-lg border p-4'>
                      <h2>
                        {t(
                          {
                            requests: 'Requests',
                            tokens: 'Tokens',
                            quota: 'Consumption',
                          }[metric]
                        )}
                      </h2>
                      <strong className='block text-2xl'>
                        {metric === 'quota'
                          ? formatQuotaWithCurrency(current)
                          : formatNumber(current)}
                      </strong>
                      <p className='text-muted-foreground text-sm'>
                        {t('Previous period')}:{' '}
                        {metric === 'quota'
                          ? formatQuotaWithCurrency(previous)
                          : formatNumber(previous)}{' '}
                        · {comparison}
                      </p>
                    </section>
                  )
                })}
              </div>
              <ConsumptionDistributionChart
                data={data.raw}
                timeGranularity='day'
                defaultChartType='bar'
              />
              <h2 className='text-lg font-semibold'>{t('Daily usage')}</h2>
              <StaticDataTable
                data={data.current.days}
                getRowKey={(row) => row.name}
                columns={[
                  { id: 'date', header: t('Date'), cell: (row) => row.name },
                  {
                    id: 'requests',
                    header: t('Requests'),
                    cell: (row) => formatNumber(row.requests),
                  },
                  {
                    id: 'tokens',
                    header: t('Tokens'),
                    cell: (row) => formatNumber(row.tokens),
                  },
                  {
                    id: 'quota',
                    header: t('Consumption'),
                    cell: (row) => formatQuotaWithCurrency(row.quota),
                  },
                ]}
              />
              <h2 className='text-lg font-semibold'>
                {t('Model consumption ranking')}
              </h2>
              <StaticDataTable
                data={data.current.models}
                getRowKey={(row) => row.name}
                columns={[
                  { id: 'model', header: t('Model'), cell: (row) => row.name },
                  {
                    id: 'requests',
                    header: t('Requests'),
                    cell: (row) => formatNumber(row.requests),
                  },
                  {
                    id: 'tokens',
                    header: t('Tokens'),
                    cell: (row) => formatNumber(row.tokens),
                  },
                  {
                    id: 'quota',
                    header: t('Consumption'),
                    cell: (row) => formatQuotaWithCurrency(row.quota),
                  },
                ]}
              />
              <h2 className='text-lg font-semibold'>
                {t('API key consumption')}
              </h2>
              <StaticDataTable
                data={data.tokens}
                getRowKey={(row) => row.id}
                columns={[
                  { id: 'name', header: t('Name'), cell: (row) => row.name },
                  {
                    id: 'requests',
                    header: t('Requests'),
                    cell: (row) => formatNumber(row.requests),
                  },
                  {
                    id: 'tokens',
                    header: t('Tokens'),
                    cell: (row) => formatNumber(row.tokens),
                  },
                  {
                    id: 'quota',
                    header: t('Consumption'),
                    cell: (row) => formatQuotaWithCurrency(row.quota),
                  },
                ]}
              />
              <section className='rounded-lg border p-4'>
                <h2>{t('Account overview')}</h2>
                <p>
                  {t('Balance')}: {formatQuotaWithCurrency(data.profile?.quota)}
                </p>
                <p>
                  {t('Historical consumption')}:{' '}
                  {formatQuotaWithCurrency(data.profile?.used_quota)}
                </p>
                <p>
                  {t('Models used')}: {formatNumber(data.current.models.length)}
                </p>
              </section>
            </>
          )}
        </article>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
