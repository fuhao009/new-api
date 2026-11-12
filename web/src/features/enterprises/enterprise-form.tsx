import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

export type EnterpriseField = {
  name: string
  label: string
  type?: 'text' | 'number' | 'password'
  options?: string[]
  value?: string | number
  min?: number
  max?: number
}
export type EnterpriseAction = {
  title: string
  action: string
  target: number
  path: string
  method: string
  fields: EnterpriseField[]
}
export function EnterpriseForm(props: {
  action: EnterpriseAction
  hidden: boolean
  pending: boolean
  onClose: () => void
  onSubmit: (values: Record<string, unknown>) => Promise<void>
}) {
  const { t } = useTranslation()
  const shape: Record<string, z.ZodType> = {}
  const defaults: Record<string, unknown> = {}
  for (const field of props.action.fields) {
    if (field.type === 'number')
      {shape[field.name] = z.coerce
        .number()
        .int()
        .min(field.min ?? 0)
        .max(field.max ?? Number.MAX_SAFE_INTEGER)}
    else if (field.options)
      {shape[field.name] = z
        .string()
        .refine((value) => field.options?.includes(value) ?? false)}
    else shape[field.name] = z.string().max(8192)
    defaults[field.name] = field.value ?? (field.type === 'number' ? 0 : '')
  }
  const form = useForm({
    resolver: zodResolver(z.object(shape)),
    defaultValues: defaults,
  })
  return (
    <Dialog
      open={!props.hidden}
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
      title={t(props.action.title)}
      description={t(
        'Enterprise changes require identity verification. Quota inputs use raw quota units.'
      )}
    >
      <form className='space-y-4' onSubmit={form.handleSubmit(props.onSubmit)}>
        {props.action.fields.map((field) => (
          <div key={field.name} className='space-y-1.5'>
            <label
              className='text-sm font-medium'
              htmlFor={`enterprise-${field.name}`}
            >
              {t(field.label)}
            </label>
            {field.options ? (
              <NativeSelect
                id={`enterprise-${field.name}`}
                {...form.register(field.name)}
              >
                {field.options.map((value) => (
                  <NativeSelectOption key={value} value={value}>
                    {t(value)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            ) : (
              <Input
                id={`enterprise-${field.name}`}
                type={field.type ?? 'text'}
                autoComplete={
                  field.type === 'password' ? 'new-password' : 'off'
                }
                {...form.register(field.name)}
              />
            )}
            {form.formState.errors[field.name] && (
              <p role='alert' className='text-destructive text-sm'>
                {t('Invalid parameters')}
              </p>
            )}
          </div>
        ))}
        <div className='flex justify-end gap-2'>
          <Button
            type='button'
            variant='outline'
            disabled={props.pending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' disabled={props.pending}>
            {t('Confirm')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
