import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { ModelGroupSelector } from '@/components/model-group-selector'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { getUserGroups, getUserModels } from '@/features/playground/api'
import { getPricing } from '@/features/pricing/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  buildCreationRequest,
  creationRequest,
  getCreationCredential,
  getCreationKeys,
  type CreationResult,
} from './api'
import { CreationResults } from './creation-results'

const schema = z.object({
  mode: z.enum(['image', 'video']),
  model: z.string().min(1),
  prompt: z.string().trim().min(1),
  size: z.string().min(1),
  seconds: z.enum(['4', '8', '12']),
})
type Values = z.infer<typeof schema>

export function Creation() {
  const { t } = useTranslation()
  const userGroup = useAuthStore((state) => state.auth.user?.group ?? '')
  const [group, setGroup] = useState('')
  const [keyId, setKeyId] = useState('')
  const [reference, setReference] = useState<File>()
  const controller = useRef<AbortController | null>(null)
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      mode: 'image',
      model: '',
      prompt: '',
      size: '1024x1024',
      seconds: '4',
    },
  })
  const values = form.watch()
  const groups = useQuery({
    queryKey: ['creation-groups'],
    queryFn: getUserGroups,
  })
  const keys = useQuery({
    queryKey: ['creation-keys'],
    queryFn: getCreationKeys,
    gcTime: 0,
  })
  const models = useQuery({
    queryKey: ['creation-models', group],
    queryFn: () => getUserModels(group),
    enabled: Boolean(group),
  })
  const pricing = useQuery({
    queryKey: ['creation-catalog'],
    queryFn: async () => requireServerSuccess(await getPricing()),
  })
  const availableKeys = (keys.data ?? []).filter(
    (key) =>
      (key.group || userGroup) === group &&
      key.status === 1 &&
      (key.expired_time === -1 || key.expired_time > Date.now() / 1000) &&
      (key.unlimited_quota || key.remain_quota > 0)
  )
  const selectedKey = availableKeys.find((key) => String(key.id) === keyId)
  const endpoint = values.mode === 'image' ? 'image-generation' : 'openai-video'
  const availableModels = (models.data ?? []).filter((model) => {
    const metadata = pricing.data?.data.find(
      (item) => item.model_name === model.value
    )
    if (!metadata?.supported_endpoint_types?.includes(endpoint)) return false
    return (
      !selectedKey?.model_limits_enabled ||
      selectedKey.model_limits?.split(',').includes(model.value)
    )
  })
  const generation = useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (input: Values) => {
      if (
        !selectedKey ||
        !availableModels.some((model) => model.value === input.model)
      )
        {throw new Error(t('Select an available API key and model'))}
      controller.current = new AbortController()
      const credential = await getCreationCredential(selectedKey.id)
      const request = buildCreationRequest(input, reference)
      const result = await creationRequest<CreationResult>(
        request.path,
        credential,
        { ...request.options, signal: controller.current.signal }
      )
      return { result, credential, video: input.mode === 'video' }
    },
  })
  useEffect(
    () => () => {
      controller.current?.abort()
    },
    []
  )
  const loading = groups.isPending || keys.isPending || pricing.isPending
  const loadError = groups.error || keys.error || models.error || pricing.error
  const sizes =
    values.mode === 'image'
      ? ['1024x1024', '1536x1024', '1024x1536', '1792x1024', '1024x1792']
      : ['1280x720', '720x1280', '1792x1024', '1024x1792']
  return (
    <div className='space-y-6'>
      <header>
        <h1 className='text-2xl font-bold'>{t('AI Creation')}</h1>
        <p className='text-muted-foreground mt-2'>
          {t(
            'Generate images and videos using your available models and personal API keys.'
          )}
        </p>
      </header>
      <div className='grid gap-6 lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)]'>
        <form
          className='space-y-5 rounded-xl border p-5'
          onSubmit={form.handleSubmit((input) => generation.mutate(input))}
        >
          <fieldset disabled={generation.isPending} className='space-y-5'>
            <div
              className='flex gap-2'
              role='group'
              aria-label={t('Creation mode')}
            >
              <Button
                type='button'
                variant={values.mode === 'image' ? 'default' : 'outline'}
                aria-pressed={values.mode === 'image'}
                onClick={() => {
                  form.setValue('mode', 'image')
                  form.setValue('size', '1024x1024')
                  form.setValue('model', '')
                }}
              >
                {t('Image')}
              </Button>
              <Button
                type='button'
                variant={values.mode === 'video' ? 'default' : 'outline'}
                aria-pressed={values.mode === 'video'}
                onClick={() => {
                  form.setValue('mode', 'video')
                  form.setValue('size', '1280x720')
                  form.setValue('model', '')
                }}
              >
                {t('Video')}
              </Button>
            </div>
            <ModelGroupSelector
              groups={groups.data ?? []}
              selectedGroup={group}
              onGroupChange={(value) => {
                setGroup(value)
                setKeyId('')
                form.setValue('model', '')
              }}
              models={availableModels}
              selectedModel={values.model}
              onModelChange={(value) => form.setValue('model', value)}
              disabled={loading}
            />
            <div className='space-y-2'>
              <Label htmlFor='creation-key'>{t('Personal API key')}</Label>
              <Select
                value={keyId}
                onValueChange={(value) => {
                  setKeyId(value ?? '')
                  form.setValue('model', '')
                }}
              >
                <SelectTrigger id='creation-key' className='w-full'>
                  <SelectValue placeholder={t('Select API key')} />
                </SelectTrigger>
                <SelectContent>
                  {availableKeys.map((key) => (
                    <SelectItem key={key.id} value={String(key.id)}>
                      {key.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {group && !loading && !availableKeys.length && (
              <p className='text-muted-foreground text-sm'>
                {t(
                  'No active personal API key with quota is available for this group. Managed accounts must use enterprise API keys outside this workspace.'
                )}
              </p>
            )}
            {group &&
              !models.isPending &&
              !pricing.isPending &&
              !availableModels.length && (
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'No compatible model is available for this mode and group.'
                  )}
                </p>
              )}
            <div className='space-y-2'>
              <Label htmlFor='creation-prompt'>{t('Prompt')}</Label>
              <Textarea
                id='creation-prompt'
                rows={6}
                required
                {...form.register('prompt')}
                aria-invalid={Boolean(form.formState.errors.prompt)}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='creation-reference'>{t('Reference image')}</Label>
              <Input
                id='creation-reference'
                type='file'
                accept='image/png,image/jpeg,image/webp'
                onChange={(event) => setReference(event.target.files?.[0])}
              />
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Optional. Image mode uses image edits; video mode uses input_reference. Support depends on the selected model.'
                )}
              </p>
            </div>
            <div className='space-y-2'>
              <Label htmlFor='creation-size'>
                {t('Resolution / aspect ratio')}
              </Label>
              <Select
                value={values.size}
                onValueChange={(value) => {
                  if (value) form.setValue('size', value)
                }}
              >
                <SelectTrigger id='creation-size' className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {sizes.map((size) => (
                    <SelectItem key={size} value={size}>
                      {size}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Supported sizes vary by model. Unsupported parameters are reported by the provider.'
                )}
              </p>
            </div>
            {values.mode === 'video' && (
              <div className='space-y-2'>
                <Label htmlFor='creation-seconds'>
                  {t('Duration (seconds)')}
                </Label>
                <Select
                  value={values.seconds}
                  onValueChange={(value) => {
                    if (value === '4' || value === '8' || value === '12')
                      {form.setValue('seconds', value)}
                  }}
                >
                  <SelectTrigger id='creation-seconds'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {['4', '8', '12'].map((seconds) => (
                      <SelectItem key={seconds} value={seconds}>
                        {seconds}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <p className='text-muted-foreground text-sm'>
              {t(
                'Submitting a generation uses your account quota. Requests are not retried automatically.'
              )}
            </p>
            <Button
              type='submit'
              className='w-full'
              disabled={
                loading ||
                !selectedKey ||
                !values.prompt.trim() ||
                !availableModels.some((model) => model.value === values.model)
              }
            >
              {generation.isPending ? t('Generating…') : t('Generate')}
            </Button>
          </fieldset>
          {generation.isPending && (
            <Button
              type='button'
              variant='outline'
              onClick={() => controller.current?.abort()}
            >
              {t('Stop waiting')}
            </Button>
          )}
          <p className='text-muted-foreground text-xs'>
            {t(
              'Stopping or leaving this page does not cancel an upstream generation or refund its cost.'
            )}
          </p>
        </form>
        <div className='min-w-0 rounded-xl border p-5'>
          {loadError && <ErrorState description={loadError.message} />}
          {generation.isError && (
            <ErrorState description={generation.error.message} />
          )}
          {generation.data && (
            <CreationResults
              key={generation.submittedAt}
              {...generation.data}
            />
          )}
          {!generation.data && !generation.isError && (
            <EmptyState
              title={
                generation.isPending
                  ? t('Generating…')
                  : t('Your creations will appear here')
              }
              description={t(
                'Only actual provider results and task statuses are displayed.'
              )}
            />
          )}
        </div>
      </div>
    </div>
  )
}
