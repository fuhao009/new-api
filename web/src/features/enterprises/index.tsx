import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { useAuthStore } from '@/stores/auth-store'

import {
  enterpriseGet,
  enterpriseMutate,
  type Enterprise,
  type EnterpriseRow,
  type Page,
} from './api'
import {
  EnterpriseForm,
  type EnterpriseAction,
  type EnterpriseField,
} from './enterprise-form'

const sections = ['members', 'keys', 'ledger', 'balance', 'audit'] as const
const sectionTitles = {
  members: 'Enterprise members',
  keys: 'Enterprise API keys',
  ledger: 'Enterprise usage ledger',
  balance: 'Enterprise balance history',
  audit: 'Enterprise audit',
}
const quotaField = (value = 0): EnterpriseField => ({
  name: 'quota_limit',
  label: 'Member quota limit (0 means unlimited)',
  type: 'number',
  value,
})
export function Enterprises() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const client = useQueryClient()
  const [selected, setSelected] = useState(0)
  const [section, setSection] = useState<(typeof sections)[number]>('members')
  const [page, setPage] = useState(1)
  const [action, setAction] = useState<EnterpriseAction | null>(null)
  const [secret, setSecret] = useState('')
  const verification = useSecureVerification()
  const list = useQuery({
    queryKey: ['enterprises', userId],
    queryFn: () =>
      enterpriseGet<{ items: Enterprise[]; site_admin: boolean }>(''),
  })
  const detail = useQuery({
    queryKey: ['enterprises', userId, selected, 'detail'],
    queryFn: () =>
      enterpriseGet<{ enterprise: Enterprise; role: string }>(`/${selected}`),
    enabled: selected > 0,
  })
  const role = detail.data?.role ?? ''
  const manager = ['owner', 'admin', 'site_admin'].includes(role)
  const rows = useQuery({
    queryKey: ['enterprises', userId, selected, section, page],
    queryFn: () =>
      enterpriseGet<Page>(`/${selected}/${section}?page=${page}&page_size=20`),
    enabled:
      selected > 0 &&
      !!role &&
      !(role === 'billing' && section === 'keys') &&
      !(role === 'member' && (section === 'balance' || section === 'audit')),
  })
  const mutation = useMutation({
    mutationFn: async (values: Record<string, unknown>) => {
      if (!action) return
      let target = action.target
      if (action.action === 'create') target = Number(values.owner_id)
      if (action.action === 'owner.transfer') target = Number(values.user_id)
      const proof = await verification.requestVerification({
        scope: 'enterprise.manage',
        context: {
          enterprise_id: action.action === 'create' ? 0 : selected,
          action: action.action,
          target_id: target,
        },
        title: t('Verify enterprise operation'),
      })
      if (!proof) return
      const payload = { ...values }
      if ('unlimited_quota' in payload)
        {payload.unlimited_quota = payload.unlimited_quota === 'true'}
      const result = await enterpriseMutate(
        action.path,
        action.method,
        payload,
        proof.proof_token
      )
      setAction(null)
      if (result?.key) setSecret(result.key)
      await client.invalidateQueries({ queryKey: ['enterprises', userId] })
    },
  })
  const open = (
    title: string,
    kind: string,
    path: string,
    fields: EnterpriseField[],
    target = 0,
    method = 'POST'
  ) => setAction({ title, action: kind, path, fields, target, method })
  const keyFields = (row?: EnterpriseRow): EnterpriseField[] => [
    ...(!row && manager
      ? [
          {
            name: 'user_id',
            label: 'Member user ID',
            type: 'number' as const,
            value: userId ?? 0,
          },
        ]
      : []),
    { name: 'name', label: 'Name', value: row?.name ?? '' },
    {
      name: 'status',
      label: 'Status',
      type: 'number',
      value: Number(row?.status ?? 1),
      min: 1,
      max: 2,
    },
    {
      name: 'expired_time',
      label: 'Expiry Unix timestamp (-1 means never)',
      type: 'number',
      value: row?.expired_time ?? -1,
      min: -1,
    },
    {
      name: 'remain_quota',
      label: 'Remaining quota',
      type: 'number',
      value: row?.remain_quota ?? 0,
    },
    {
      name: 'unlimited_quota',
      label: 'Unlimited quota',
      options: ['false', 'true'],
      value: String(row?.unlimited_quota ?? true),
    },
    {
      name: 'model_limits',
      label: 'Allowed models (comma separated)',
      value: row?.model_limits ?? '',
    },
    {
      name: 'allow_ips',
      label: 'Allowed IP addresses',
      value: row?.allow_ips ?? '',
    },
  ]
  const columns: StaticDataTableColumn<EnterpriseRow>[] = []
  if (section === 'members') {
    columns.push(
      {
        id: 'username',
        header: t('Username'),
        cell: (row) => `${row.username ?? row.user_id} (#${row.user_id})`,
      },
      { id: 'role', header: t('Role'), cell: (row) => t(row.role ?? '') },
      {
        id: 'status',
        header: t('Status'),
        cell: (row) => t(String(row.status)),
      },
      {
        id: 'managed',
        header: t('Managed account'),
        cell: (row) => t(row.managed ? 'Yes' : 'No'),
      },
      {
        id: 'quota_limit',
        header: t('Quota limit'),
        cell: (row) =>
          row.quota_limit === 0
            ? t('Unlimited')
            : formatQuotaWithCurrency(row.quota_limit),
      },
      {
        id: 'used_quota',
        header: t('Used quota'),
        cell: (row) => formatQuotaWithCurrency(row.used_quota),
      }
    )
    if (manager)
      {columns.push({
        id: 'actions',
        header: t('Actions'),
        cell: (row) =>
          row.role === 'owner' ? null : (
            <Button
              size='sm'
              variant='outline'
              onClick={() =>
                open(
                  'Edit enterprise member',
                  'member.update',
                  `/${selected}/members/${row.user_id}`,
                  [
                    {
                      name: 'role',
                      label: 'Role',
                      options:
                        role === 'admin'
                          ? ['member']
                          : ['admin', 'billing', 'member'],
                      value: row.role,
                    },
                    {
                      name: 'status',
                      label: 'Status',
                      options: ['active', 'suspended', 'removed'],
                      value: row.status,
                    },
                    quotaField(row.quota_limit),
                  ],
                  row.user_id,
                  'PATCH'
                )
              }
            >
              {t('Edit')}
            </Button>
          ),
      })}
  } else if (section === 'keys') {
    columns.push(
      { id: 'name', header: t('Name'), cell: (row) => row.name },
      {
        id: 'user_id',
        header: t('Member user ID'),
        cell: (row) => row.user_id,
      },
      {
        id: 'status',
        header: t('Status'),
        cell: (row) => (row.status === 1 ? t('Enabled') : t('Disabled')),
      },
      {
        id: 'used_quota',
        header: t('Used quota'),
        cell: (row) => formatQuotaWithCurrency(row.used_quota),
      },
      {
        id: 'actions',
        header: t('Actions'),
        cell: (row) => (
          <div className='flex gap-2'>
            <Button
              size='sm'
              variant='outline'
              onClick={() =>
                open(
                  'Edit enterprise API key',
                  'key.update',
                  `/${selected}/keys/${row.id}`,
                  keyFields(row),
                  row.id,
                  'PATCH'
                )
              }
            >
              {t('Edit')}
            </Button>
            <Button
              size='sm'
              variant='destructive'
              onClick={() =>
                open(
                  'Revoke enterprise API key',
                  'key.delete',
                  `/${selected}/keys/${row.id}`,
                  [],
                  row.id,
                  'DELETE'
                )
              }
            >
              {t('Revoke')}
            </Button>
          </div>
        ),
      }
    )
  } else {
    let fields = ['actor_id', 'target_id', 'action', 'detail', 'created_at']
    if (section === 'ledger')
      {fields = ['request_id', 'user_id', 'amount', 'status', 'created_at']}
    if (section === 'balance')
      {fields = ['actor_id', 'amount', 'balance', 'reason', 'created_at']}
    for (const field of fields) {
      columns.push({
        id: field,
        header: t(field),
        cell: (row) => {
          const value = row[field as keyof EnterpriseRow]
          if (field === 'amount' || field === 'balance')
            {return formatQuotaWithCurrency(Number(value ?? 0))}
          if (field === 'created_at')
            {return new Date(Number(value) * 1000).toLocaleString()}
          return String(value ?? '')
        },
      })
    }
  }
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Enterprise management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {list.data?.site_admin && (
          <Button
            onClick={() =>
              open(
                'Create enterprise',
                'create',
                '',
                [
                  { name: 'name', label: 'Enterprise name' },
                  {
                    name: 'owner_id',
                    label: 'Owner user ID',
                    type: 'number',
                    value: userId ?? 0,
                    min: 1,
                  },
                ],
                userId ?? 0
              )
            }
          >
            {t('Create enterprise')}
          </Button>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Enterprise balances and permissions are separate from personal accounts.'
            )}
          </p>
          {list.isError && <p role='alert'>{t('Failed to load data')}</p>}
          <NativeSelect
            aria-label={t('Select enterprise')}
            value={selected}
            onChange={(event) => {
              setSelected(Number(event.target.value))
              setSection('members')
              setPage(1)
            }}
          >
            <NativeSelectOption value={0}>
              {t('Select enterprise')}
            </NativeSelectOption>
            {list.data?.items.map((item) => (
              <NativeSelectOption key={item.id} value={item.id}>
                {item.name} · {t(item.status)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          {detail.isError && (
            <p role='alert'>{t('Enterprise access denied')}</p>
          )}
          {detail.data && selected > 0 && (
            <>
              <div className='flex flex-wrap items-center gap-4 rounded-lg border p-4'>
                <strong>{detail.data.enterprise.name}</strong>
                <span>{t(role)}</span>
                {role !== 'member' && (
                  <>
                    <span>
                      {t('Balance')}:{' '}
                      {formatQuotaWithCurrency(detail.data.enterprise.balance)}
                    </span>
                    <span>
                      {t('Used quota')}:{' '}
                      {formatQuotaWithCurrency(
                        detail.data.enterprise.used_quota
                      )}
                    </span>
                  </>
                )}
              </div>
              <div className='flex flex-wrap gap-2'>
                {manager && (
                  <>
                    <Button
                      variant='outline'
                      onClick={() =>
                        open(
                          'Add enterprise member',
                          'member.add',
                          `/${selected}/members`,
                          [
                            { name: 'username', label: 'Username' },
                            {
                              name: 'role',
                              label: 'Role',
                              options:
                                role === 'admin'
                                  ? ['member']
                                  : ['member', 'admin', 'billing'],
                              value: 'member',
                            },
                            quotaField(),
                          ]
                        )
                      }
                    >
                      {t('Add enterprise member')}
                    </Button>
                    <Button
                      variant='outline'
                      onClick={() =>
                        open(
                          'Create managed account',
                          'managed.create',
                          `/${selected}/managed-accounts`,
                          [
                            { name: 'username', label: 'Username' },
                            { name: 'display_name', label: 'Display name' },
                            {
                              name: 'password',
                              label: 'Password (at least 15 characters)',
                              type: 'password',
                            },
                            quotaField(),
                          ]
                        )
                      }
                    >
                      {t('Create managed account')}
                    </Button>
                    <Button
                      variant='outline'
                      onClick={() =>
                        open(
                          'Enterprise settings',
                          'update',
                          `/${selected}`,
                          [
                            {
                              name: 'name',
                              label: 'Enterprise name',
                              value: detail.data.enterprise.name,
                            },
                            {
                              name: 'status',
                              label: 'Status',
                              options:
                                role === 'site_admin'
                                  ? ['active', 'suspended']
                                  : [detail.data.enterprise.status],
                              value: detail.data.enterprise.status,
                            },
                          ],
                          0,
                          'PATCH'
                        )
                      }
                    >
                      {t('Enterprise settings')}
                    </Button>
                  </>
                )}
                {(role === 'owner' || role === 'site_admin') && (
                  <Button
                    variant='outline'
                    onClick={() =>
                      open(
                        'Transfer enterprise ownership',
                        'owner.transfer',
                        `/${selected}/owner-transfer`,
                        [
                          {
                            name: 'user_id',
                            label: 'New owner user ID',
                            type: 'number',
                            min: 1,
                          },
                        ]
                      )
                    }
                  >
                    {t('Transfer enterprise ownership')}
                  </Button>
                )}
                {role === 'site_admin' && (
                  <Button
                    variant='outline'
                    onClick={() =>
                      open(
                        'Adjust enterprise balance',
                        'balance.adjust',
                        `/${selected}/balance`,
                        [
                          {
                            name: 'amount',
                            label: 'Quota adjustment',
                            type: 'number',
                            min: -Number.MAX_SAFE_INTEGER,
                          },
                          { name: 'reason', label: 'Reason' },
                        ]
                      )
                    }
                  >
                    {t('Adjust enterprise balance')}
                  </Button>
                )}
                {role !== 'billing' && (
                  <Button
                    onClick={() =>
                      open(
                        'Create enterprise API key',
                        'key.create',
                        `/${selected}/keys`,
                        keyFields()
                      )
                    }
                  >
                    {t('Create enterprise API key')}
                  </Button>
                )}
              </div>
              <nav
                className='flex flex-wrap gap-2'
                aria-label={t('Enterprise sections')}
              >
                {sections
                  .filter(
                    (item) =>
                      !(role === 'billing' && item === 'keys') &&
                      !(
                        role === 'member' &&
                        (item === 'audit' || item === 'balance')
                      )
                  )
                  .map((item) => (
                    <Button
                      key={item}
                      variant={section === item ? 'default' : 'outline'}
                      onClick={() => {
                        setSection(item)
                        setPage(1)
                      }}
                    >
                      {t(sectionTitles[item])}
                    </Button>
                  ))}
              </nav>
              {rows.isPending && <p>{t('Loading...')}</p>}
              {rows.isError && <p role='alert'>{t('Failed to load data')}</p>}
              {rows.isSuccess && (
                <StaticDataTable
                  columns={columns}
                  data={rows.data.items ?? []}
                  getRowKey={(row) => row.id}
                  emptyContent={t('No data')}
                />
              )}
              <div className='flex items-center justify-end gap-3'>
                <Button
                  variant='outline'
                  disabled={page === 1}
                  onClick={() => setPage(page - 1)}
                >
                  {t('Previous')}
                </Button>
                <span>{page}</span>
                <Button
                  variant='outline'
                  disabled={page * 20 >= (rows.data?.total ?? 0)}
                  onClick={() => setPage(page + 1)}
                >
                  {t('Next')}
                </Button>
              </div>
            </>
          )}
        </div>
        {action && (
          <EnterpriseForm
            key={`${action.action}-${action.target}`}
            action={action}
            hidden={verification.isActive}
            pending={mutation.isPending}
            onClose={() => setAction(null)}
            onSubmit={async (values) => {
              await mutation.mutateAsync(values)
            }}
          />
        )}
        <SecureVerificationDialog {...verification.dialogProps} />
        <Dialog
          open={!!secret}
          onOpenChange={(open) => {
            if (!open) setSecret('')
          }}
          title={t('Save your enterprise API key')}
          description={t('This secret is shown only once. Store it securely.')}
        >
          <div className='flex items-center gap-2'>
            <code className='break-all'>{secret}</code>
            <CopyButton value={secret} />
          </div>
        </Dialog>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
