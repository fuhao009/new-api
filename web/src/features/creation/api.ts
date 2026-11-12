import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type CreationMode = 'image' | 'video'
export type CreationResult = {
  id?: string
  status?: string
  progress?: number
  error?: { message?: string } | string
  data?: { url?: string; b64_json?: string; revised_prompt?: string }[]
}

export async function getCreationKeys() {
  const items = []
  let page = 1
  for (;;) {
    const response = requireServerSuccess(
      await getApiKeys({ p: page, size: 100 })
    )
    const batch = response.data?.items ?? []
    // Never retain the list endpoint's readable credentials in the query cache.
    items.push(...batch.map(({ key: _key, ...item }) => item))
    if (!batch.length || items.length >= (response.data?.total ?? 0))
      {return items}
    page += 1
  }
}

export async function getCreationCredential(id: number): Promise<string> {
  const response = requireServerSuccess(await fetchTokenKey(id))
  if (!response.data?.key) throw new Error('API key is unavailable')
  const key = response.data.key
  return key.startsWith('sk-') ? key : `sk-${key}`
}

// The console client overwrites Authorization with its session access token.
// Relay calls must instead use only the selected API key and must never replay.
export async function creationRequest<T>(
  path: string,
  key: string,
  options: RequestInit = {}
): Promise<T> {
  const response = await fetch(path, {
    ...options,
    credentials: 'omit',
    cache: 'no-store',
    headers: { ...options.headers, Authorization: `Bearer ${key}` },
  })
  const text = await response.text()
  let data: T & { error?: { message?: string }; message?: string }
  try {
    data = JSON.parse(text) as typeof data
  } catch {
    throw new Error(
      `HTTP ${response.status}: ${text.slice(0, 500) || 'Empty provider response'}`
    )
  }
  if (!data || typeof data !== 'object')
    {throw new Error('Invalid provider response')}
  if (!response.ok || data.error) {
    throw new Error(
      data.error?.message || data.message || `HTTP ${response.status}`
    )
  }
  return data
}

export function buildCreationRequest(
  values: {
    mode: CreationMode
    model: string
    prompt: string
    size: string
    seconds: string
  },
  reference?: File
): { path: string; options: RequestInit } {
  const path = values.mode === 'video' ? '/v1/videos' : '/v1/images/generations'
  if (reference) {
    const body = new FormData()
    body.set('model', values.model)
    body.set('prompt', values.prompt)
    body.set('size', values.size)
    if (values.mode === 'video') body.set('seconds', values.seconds)
    body.set(values.mode === 'video' ? 'input_reference' : 'image', reference)
    return {
      path: values.mode === 'image' ? '/v1/images/edits' : path,
      options: { method: 'POST', body },
    }
  }
  const body: Record<string, string | number> = {
    model: values.model,
    prompt: values.prompt,
    size: values.size,
  }
  if (values.mode === 'video') body.seconds = values.seconds
  else body.n = 1
  return {
    path,
    options: {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  }
}

export function isCreationPending(result?: CreationResult): boolean {
  return Boolean(
    result?.id &&
    ['queued', 'in_progress', 'pending', 'processing'].includes(
      result.status?.toLowerCase() ?? ''
    )
  )
}
