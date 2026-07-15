/** Payment gateway admin config (contract C3, FR-23). Creds never round-trip. */
import { request } from './client'

export interface GatewayConfig {
  gateway: string
  enabled: boolean
  mode: 'live' | 'mock'
  configured: boolean
}

export function listGatewayConfigs(): Promise<{ items: GatewayConfig[] }> {
  return request<{ items: GatewayConfig[] }>('/payment-gateways')
}

export function putGatewayConfig(
  gateway: string,
  body: { enabled: boolean; mode?: string; creds?: Record<string, unknown> },
): Promise<{ gateway: string; enabled: boolean }> {
  return request(`/payment-gateways/${gateway}`, { method: 'PUT', body })
}
