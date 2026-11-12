import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

type Device = {
  id: number
  name: string
  user_agent: string
  created_at: number
  expires_at: number
  last_used_at: number
}
export function TrustedDevicesCard() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const client = useQueryClient()
  const verification = useSecureVerification()
  const [name, setName] = useState('')
  const [revoke, setRevoke] = useState<Device | null>(null)
  const query = useQuery({
    queryKey: ['trusted-devices', userId],
    queryFn: async () => {
      const response = await api.get<{ success: boolean; data: Device[] }>(
        '/api/user/trusted-devices'
      )
      requireServerSuccess(response.data)
      return response.data.data
    },
  })
  const enroll = useMutation({
    mutationFn: async () => {
      const proof = await verification.requestVerification({
        scope: 'trusted_device.enroll',
        title: t('Trust this browser'),
      })
      if (!proof) return
      const response = await api.post(
        '/api/user/trusted-devices',
        { name },
        { headers: { 'X-Security-Proof': proof.proof_token } }
      )
      requireServerSuccess(response.data)
      setName('')
      await client.invalidateQueries({ queryKey: ['trusted-devices', userId] })
    },
  })
  const remove = useMutation({
    mutationFn: async () => {
      if (!revoke) return
      const response = await api.delete(
        `/api/user/trusted-devices/${revoke.id}`
      )
      requireServerSuccess(response.data)
      setRevoke(null)
      await client.invalidateQueries({ queryKey: ['trusted-devices', userId] })
    },
  })
  return (
    <section className='space-y-3 rounded-lg border p-4'>
      <h3 className='font-semibold'>{t('Trusted devices')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Trusted browsers can bypass account password lockout for 30 days. Correct password, MFA and device/IP rate limits still apply. Password changes invalidate device trust.'
        )}
      </p>
      <form
        className='flex gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          enroll.mutate()
        }}
      >
        <Input
          aria-label={t('Device name')}
          placeholder={t('Device name')}
          value={name}
          onChange={(event) => setName(event.target.value)}
          required
          maxLength={128}
        />
        <Button type='submit' disabled={enroll.isPending}>{t('Trust this browser')}</Button>
      </form>
      {query.isError && <p role='alert'>{t('Failed to load data')}</p>}
      <StaticDataTable
        data={query.data ?? []}
        getRowKey={(row) => row.id}
        columns={[
          { id: 'name', header: t('Name'), cell: (row) => row.name },
          {
            id: 'expires',
            header: t('Expires at'),
            cell: (row) => new Date(row.expires_at * 1000).toLocaleString(),
          },
          {
            id: 'actions',
            header: t('Actions'),
            cell: (row) => (
              <Button variant='outline' onClick={() => setRevoke(row)}>
                {t('Revoke')}
              </Button>
            ),
          },
        ]}
      />
      <ConfirmDialog
        open={!!revoke}
        onOpenChange={(open) => {
          if (!open) setRevoke(null)
        }}
        title={t('Revoke trusted device')}
        desc={revoke?.name ?? ''}
        destructive
        isLoading={remove.isPending}
        handleConfirm={() => remove.mutate()}
      />
      <SecureVerificationDialog {...verification.dialogProps} />
    </section>
  )
}
