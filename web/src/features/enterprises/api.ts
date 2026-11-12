import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type Enterprise = {
  id: number
  name: string
  status: string
  owner_id: number
  balance: number
  used_quota: number
}
export type EnterpriseRow = {
  id: number
  user_id?: number
  username?: string
  display_name?: string
  name?: string
  role?: string
  status?: string | number
  managed?: boolean
  quota_limit?: number
  used_quota?: number
  remain_quota?: number
  unlimited_quota?: boolean
  expired_time?: number
  model_limits?: string
  allow_ips?: string
  request_id?: string
  amount?: number
  balance?: number
  action?: string
  actor_id?: number
  target_id?: number
  detail?: string
  reason?: string
  created_at?: number
}
export type Page = {
  items: EnterpriseRow[]
  total: number
  page: number
  page_size: number
}
export async function enterpriseGet<T>(path: string): Promise<T> {
  const response = await api.get<{
    success: boolean
    message?: string
    data: T
  }>(`/api/enterprises${path}`)
  requireServerSuccess(response.data)
  return response.data.data
}
export async function enterpriseMutate(
  path: string,
  method: string,
  data: Record<string, unknown>,
  proof: string
): Promise<{ key?: string } | null> {
  const response = await api.request<{
    success: boolean
    message?: string
    data: { key?: string } | null
  }>({
    url: `/api/enterprises${path}`,
    method,
    data,
    headers: { 'X-Security-Proof': proof },
  })
  requireServerSuccess(response.data)
  return response.data.data
}
