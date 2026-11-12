import { createFileRoute } from '@tanstack/react-router'

import { Enterprises } from '@/features/enterprises'

export const Route = createFileRoute('/_authenticated/enterprises/')({
  component: Enterprises,
})
