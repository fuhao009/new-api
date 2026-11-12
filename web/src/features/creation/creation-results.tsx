import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'

import { creationRequest, isCreationPending, type CreationResult } from './api'

export function CreationResults(props: {
  result: CreationResult
  credential: string
  video: boolean
}) {
  const { t } = useTranslation()
  const requestScope = useId()
  const [videoUrl, setVideoUrl] = useState('')
  const task = useQuery({
    queryKey: ['creation-task', requestScope, props.result.id],
    queryFn: ({ signal }) =>
      creationRequest<CreationResult>(
        `/v1/videos/${encodeURIComponent(props.result.id ?? '')}`,
        props.credential,
        { signal }
      ),
    enabled: props.video && Boolean(props.result.id),
    initialData: props.result,
    gcTime: 0,
    retry: false,
    refetchInterval: (query) =>
      isCreationPending(query.state.data) ? 3000 : false,
  })
  const result = props.video ? task.data : props.result
  const content = useMutation({
    mutationFn: async () => {
      if (!props.result.id) throw new Error('Missing task ID')
      const response = await fetch(
        `/v1/videos/${encodeURIComponent(props.result.id)}/content`,
        {
          headers: { Authorization: `Bearer ${props.credential}` },
          credentials: 'omit',
          cache: 'no-store',
        }
      )
      if (!response.ok) throw new Error(await response.text())
      return URL.createObjectURL(await response.blob())
    },
    onSuccess: setVideoUrl,
  })
  useEffect(
    () => () => {
      if (videoUrl) URL.revokeObjectURL(videoUrl)
    },
    [videoUrl]
  )
  const failed = result.status === 'failed' || result.status === 'cancelled'
  const error =
    typeof result.error === 'string' ? result.error : result.error?.message
  return (
    <section className='space-y-4' aria-live='polite'>
      <h2 className='text-lg font-semibold'>{t('Creation results')}</h2>
      {result.id && (
        <p className='text-sm break-all'>
          {t('Task ID')}: {result.id}
        </p>
      )}
      {result.status && (
        <p>
          {t('Status')}: {result.status}
          {typeof result.progress === 'number' && ` · ${result.progress}%`}
        </p>
      )}
      {isCreationPending(result) && (
        <p className='text-muted-foreground'>
          {t(
            'Generation is in progress. This page checks the task automatically.'
          )}
        </p>
      )}
      {failed && <ErrorState description={error || t('Generation failed')} />}
      {task.isError && props.video && (
        <ErrorState
          description={task.error.message}
          onRetry={() => {
            void task.refetch()
          }}
        />
      )}
      {result.data?.map((image) => {
        const source = image.b64_json
          ? `data:image/png;base64,${image.b64_json}`
          : image.url
        if (
          !source ||
          (!source.startsWith('https://') &&
            !source.startsWith('http://') &&
            !source.startsWith('data:image/'))
        )
          {return null}
        return (
          <figure key={source} className='space-y-2'>
            <img
              src={source}
              alt={image.revised_prompt || t('Generated image')}
              className='max-h-[36rem] w-full rounded-lg object-contain'
            />
            {image.revised_prompt && (
              <figcaption className='text-muted-foreground text-sm'>
                {image.revised_prompt}
              </figcaption>
            )}
          </figure>
        )
      })}
      {props.video && result.status === 'completed' && (
        <Button disabled={content.isPending} onClick={() => content.mutate()}>
          {t('Load generated video')}
        </Button>
      )}
      {content.isError && <ErrorState description={content.error.message} />}
      {videoUrl && (
        <video src={videoUrl} controls className='w-full rounded-lg'>
          <track kind='captions' />
        </video>
      )}
      {!props.video &&
        !result.data?.some((image) => image.url || image.b64_json) && (
          <EmptyState title={t('The provider returned no image output')} />
        )}
    </section>
  )
}
