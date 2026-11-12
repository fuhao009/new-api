import { createFileRoute } from '@tanstack/react-router'
import { Main } from '@/components/layout'
import { Creation } from '@/features/creation'

export const Route = createFileRoute('/_authenticated/creation/')({
  component: CreationPage,
})

function CreationPage() {
  return <Main><Creation /></Main>
}
