import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  buildCreationRequest,
  creationRequest,
  isCreationPending,
} from '../api'

afterEach(() => vi.unstubAllGlobals())

describe('creation relay requests', () => {
  it('submits text to image through the real generation endpoint', () => {
    const request = buildCreationRequest({
      mode: 'image',
      model: 'image-model',
      prompt: 'A tree',
      size: '1024x1024',
      seconds: '4',
    })
    expect(request.path).toBe('/v1/images/generations')
    expect(JSON.parse(request.options.body as string)).toEqual({
      model: 'image-model',
      prompt: 'A tree',
      size: '1024x1024',
      n: 1,
    })
  })
  it('submits reference images as multipart edits rather than silently ignoring them', () => {
    const reference = new File(['image'], 'reference.png', {
      type: 'image/png',
    })
    const request = buildCreationRequest(
      {
        mode: 'image',
        model: 'image-model',
        prompt: 'A tree',
        size: '1024x1024',
        seconds: '4',
      },
      reference
    )
    expect(request.path).toBe('/v1/images/edits')
    expect((request.options.body as FormData).get('image')).toBe(reference)
  })
  it('submits video references and duration through the video protocol', () => {
    const reference = new File(['image'], 'reference.png', {
      type: 'image/png',
    })
    const request = buildCreationRequest(
      {
        mode: 'video',
        model: 'video-model',
        prompt: 'A tree',
        size: '1280x720',
        seconds: '8',
      },
      reference
    )
    expect(request.path).toBe('/v1/videos')
    expect((request.options.body as FormData).get('input_reference')).toBe(
      reference
    )
    expect((request.options.body as FormData).get('seconds')).toBe('8')
  })
  it('uses only the selected credential and forwards cancellation without replay', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ id: 'task', status: 'queued' }))
      )
    vi.stubGlobal('fetch', fetcher)
    const controller = new AbortController()
    await creationRequest('/v1/videos', 'sk-personal', {
      method: 'POST',
      signal: controller.signal,
    })
    expect(fetcher).toHaveBeenCalledExactlyOnceWith(
      '/v1/videos',
      expect.objectContaining({
        credentials: 'omit',
        cache: 'no-store',
        headers: { Authorization: 'Bearer sk-personal' },
        signal: controller.signal,
      })
    )
  })
  it('surfaces provider rejection and never retries a chargeable request', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ error: { message: 'Insufficient quota' } }),
          { status: 403 }
        )
      )
    vi.stubGlobal('fetch', fetcher)
    await expect(
      creationRequest('/v1/videos', 'sk-personal', { method: 'POST' })
    ).rejects.toThrow('Insufficient quota')
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
  it('does not poll completed or failed tasks', () => {
    expect(isCreationPending({ id: 'task', status: 'in_progress' })).toBe(true)
    expect(isCreationPending({ id: 'task', status: 'completed' })).toBe(false)
    expect(isCreationPending({ id: 'task', status: 'failed' })).toBe(false)
  })
})
