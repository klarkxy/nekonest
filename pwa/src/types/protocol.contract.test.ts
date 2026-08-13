import { describe, expect, it } from 'vitest'
import schema from '../../../protocol/protocol.json'
import { SERVICE_ERROR_CODES } from './protocol'

type ProtocolSchema = {
  definitions: {
    ServiceErrorCode: { enum: string[] }
    ServiceErrorPayload: { required: string[] }
    DeviceRegistrationResponse: {
      required: string[]
      properties: { connection_state: { enum: string[] } }
    }
  }
}

describe('public protocol contract', () => {
  const contract = schema as unknown as ProtocolSchema

  it('keeps the PWA stable error catalog aligned with the schema', () => {
    expect([...SERVICE_ERROR_CODES]).toEqual(contract.definitions.ServiceErrorCode.enum)
  })

  it('requires the common error envelope and stable registration state', () => {
    expect(contract.definitions.ServiceErrorPayload.required).toEqual([
      'error_code',
      'message',
      'retryable'
    ])
    expect(contract.definitions.DeviceRegistrationResponse.required).toContain('connection_state')
    expect(contract.definitions.DeviceRegistrationResponse.properties.connection_state.enum).toEqual([
      'ready',
      'provisioning'
    ])
  })
})
