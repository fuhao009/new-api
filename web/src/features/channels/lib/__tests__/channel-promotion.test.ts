/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from '../channel-form'

function channelForm(
  overrides: Partial<ChannelFormValues> = {}
): ChannelFormValues {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Promo channel',
    type: 1,
    key: 'test-key',
    models: 'gpt-4.1',
    ...overrides,
  }
}

function savedSettings(form: ChannelFormValues): Record<string, unknown> {
  const payload = transformFormDataToCreatePayload(
    channelFormSchema.parse(form)
  )
  return JSON.parse(payload.channel.settings ?? '{}')
}

function editableChannel(settings: Record<string, unknown>) {
  return channelSchema.parse({
    id: 9,
    name: 'Promo channel',
    type: 1,
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'gpt-4.1',
    group: 'default',
    priority: 3,
    weight: 5,
    settings: JSON.stringify(settings),
  })
}

describe('channel promotion ratio settings', () => {
  test('defaults to the original price when the field is untouched', () => {
    expect(CHANNEL_FORM_DEFAULT_VALUES.channel_ratio).toBe(1)
    expect(savedSettings(channelForm())).not.toHaveProperty('channel_ratio')
  })

  test('saves a discount without touching key, group, model, or other settings', () => {
    const payload = transformFormDataToCreatePayload(
      channelFormSchema.parse(
        channelForm({
          channel_ratio: 0.8,
          group: ['default', 'premium'],
          model_mapping: '{"alias":"gpt-4.1"}',
          settings: JSON.stringify({ custom_option: true }),
        })
      )
    )
    const settings = JSON.parse(payload.channel.settings ?? '{}')

    expect(payload.channel).toMatchObject({
      key: 'test-key',
      models: 'gpt-4.1',
      group: 'default,premium',
      model_mapping: '{"alias":"gpt-4.1"}',
    })
    expect(settings).toMatchObject({
      channel_ratio: 0.8,
      custom_option: true,
    })
  })

  test('keeps a zero ratio so the channel stays free', () => {
    expect(savedSettings(channelForm({ channel_ratio: 0 }))).toMatchObject({
      channel_ratio: 0,
    })
  })

  test('drops a stored discount after the ratio returns to 1', () => {
    const settings = savedSettings(
      channelForm({
        channel_ratio: 1,
        settings: JSON.stringify({ channel_ratio: 0.8, custom_option: true }),
      })
    )

    expect(settings).toMatchObject({ custom_option: true })
    expect(settings).not.toHaveProperty('channel_ratio')
  })

  test('loads the stored ratio and defaults missing or invalid values to 1', () => {
    expect(
      transformChannelToFormDefaults(editableChannel({ channel_ratio: 0.8 }))
        .channel_ratio
    ).toBe(0.8)
    expect(
      transformChannelToFormDefaults(editableChannel({ channel_ratio: 0 }))
        .channel_ratio
    ).toBe(0)
    expect(
      transformChannelToFormDefaults(editableChannel({})).channel_ratio
    ).toBe(1)
    expect(
      transformChannelToFormDefaults(editableChannel({ channel_ratio: 1.5 }))
        .channel_ratio
    ).toBe(1)
    expect(
      transformChannelToFormDefaults(editableChannel({ channel_ratio: '0.8' }))
        .channel_ratio
    ).toBe(1)
  })

  test('keeps the saved ratio on update payloads', () => {
    const payload = transformFormDataToUpdatePayload(
      channelFormSchema.parse(channelForm({ channel_ratio: 0.8 })),
      9
    )

    expect(payload).toMatchObject({
      id: 9,
      models: 'gpt-4.1',
      group: 'default',
      settings: expect.stringContaining('"channel_ratio":0.8'),
    })
  })

  test.each([0.8, 0, 1])('accepts %s as a promotion ratio', (ratio) => {
    expect(
      channelFormSchema.safeParse(channelForm({ channel_ratio: ratio })).success
    ).toBe(true)
  })

  test.each([-0.1, 1.01, Number.POSITIVE_INFINITY, Number.NaN])(
    'rejects %s with the backend validation message',
    (ratio) => {
      const result = channelFormSchema.safeParse(
        channelForm({ channel_ratio: ratio })
      )

      expect(result.success).toBe(false)
      if (!result.success) {
        expect(result.error.issues).toEqual(
          expect.arrayContaining([
            expect.objectContaining({
              path: ['channel_ratio'],
              message: 'Channel promotion ratio must be between 0 and 1',
            }),
          ])
        )
      }
    }
  )
})
