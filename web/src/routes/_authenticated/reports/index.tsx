import { createFileRoute } from '@tanstack/react-router'

import { UsageReports } from '@/features/reports'
export const Route = createFileRoute('/_authenticated/reports/')({
  component: UsageReports,
})
