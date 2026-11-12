import { useMutation } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { ApiKey } from '../types'
import { useApiKeys } from './api-keys-provider'

export function ApiKeysBatchEdit<TData>(props: { table: Table<TData> }) {
  const { t } = useTranslation()
  const { triggerRefresh } = useApiKeys()
  const [open, setOpen] = useState(false)
  const [operation, setOperation] = useState('category')
  const [value, setValue] = useState('')
  const [unlimited, setUnlimited] = useState('false')
  const ids = props.table
    .getFilteredSelectedRowModel()
    .rows.map((row) => (row.original as ApiKey).id)
  const mutation = useMutation({
    mutationFn: async () => {
      const payload: Record<string, unknown> = { ids, operation }
      if (operation === 'category') payload.category = value
      if (operation === 'group') payload.group = value
      if (operation === 'models') payload.model_limits = value
      if (operation === 'quota') {
        payload.remain_quota = Number(value)
        payload.unlimited_quota = unlimited === 'true'
      }
      const response = await api.post('/api/token/batch/update', payload)
      requireServerSuccess(response.data)
    },
    onSuccess: () => {
      setOpen(false)
      props.table.resetRowSelection()
      triggerRefresh()
    },
  })
  return (
    <>
      <Button variant='outline' size='sm' onClick={() => setOpen(true)}>
        {t('Batch edit')}
      </Button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('Batch edit API keys')}
        description={t(
          'Changes apply atomically to the selected personal API keys.'
        )}
      >
        <form
          className='space-y-4'
          onSubmit={(event) => {
            event.preventDefault()
            mutation.mutate()
          }}
        >
          <label className='block space-y-2'>
            {t('Operation')}
            <NativeSelect
              value={operation}
              onChange={(event) => {
                setOperation(event.target.value)
                setValue('')
              }}
            >
              <NativeSelectOption value='category'>
                {t('Category')}
              </NativeSelectOption>
              <NativeSelectOption value='quota'>
                {t('Remaining quota')}
              </NativeSelectOption>
              <NativeSelectOption value='group'>
                {t('Group')}
              </NativeSelectOption>
              <NativeSelectOption value='models'>
                {t('Model limits')}
              </NativeSelectOption>
            </NativeSelect>
          </label>
          <label className='block space-y-2'>
            {t('New value')}
            <Input
              value={value}
              onChange={(event) => setValue(event.target.value)}
              type={operation === 'quota' ? 'number' : 'text'}
              min={0}
              maxLength={operation === 'category' ? 64 : 8192}
              required={operation === 'quota'}
            />
          </label>
          {operation === 'quota' && (
            <label className='block space-y-2'>
              {t('Unlimited quota')}
              <NativeSelect
                value={unlimited}
                onChange={(event) => setUnlimited(event.target.value)}
              >
                <NativeSelectOption value='false'>{t('No')}</NativeSelectOption>
                <NativeSelectOption value='true'>{t('Yes')}</NativeSelectOption>
              </NativeSelect>
            </label>
          )}
          <p className='text-muted-foreground text-sm'>
            {t(
              'Quota values use raw quota units. Empty category clears classification; empty model limits allows all models. Changing group resets Auto selections.'
            )}
          </p>
          <div className='flex justify-end gap-2'>
            <Button
              type='button'
              variant='outline'
              onClick={() => setOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              disabled={
                mutation.isPending || ids.length === 0 || ids.length > 100
              }
            >
              {t('Apply')}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  )
}
