import { describe, expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

describe('channel billing multiplier', () => {
  test.each([0, 0.2, 1, 1.5, 20])(
    'preserves multiplier %s across save and reload',
    (ratio) => {
      const form = channelFormSchema.parse({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        name: 'ratio',
        type: 1,
        key: 'test-key',
        models: 'test-model',
        channel_ratio: ratio,
      })
      const payload = transformFormDataToCreatePayload(form)
      const channel = channelSchema.parse({
        ...payload.channel,
        other: '',
        remark: '',
        id: 7,
        status: 1,
        created_time: 1,
        test_time: 0,
        response_time: 0,
        balance_updated_time: 0,
      })
      expect(transformChannelToFormDefaults(channel).channel_ratio).toBe(ratio)
    }
  )
  test.each([-1, NaN, Infinity])('rejects invalid multiplier %s', (ratio) => {
    expect(
      channelFormSchema.safeParse({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        name: 'ratio',
        type: 1,
        key: 'test-key',
        models: 'test-model',
        channel_ratio: ratio,
      }).success
    ).toBe(false)
  })
})
