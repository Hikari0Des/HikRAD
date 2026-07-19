/**
 * Payment provider catalog + per-manager accounts/methods — v2-2 contracts
 * C1-C4 (FR-77). Providers are read by every manager (to configure their own
 * account) but only writable under payment_providers.manage; accounts/method
 * settings are writable by the manager themselves or an admin.
 */
import { request } from './client'

export interface PaymentProvider {
  id: string
  name: string
  logo_path?: string | null
  instructions_template: string
  enabled: boolean
}

export function listProviders(): Promise<{ items: PaymentProvider[] }> {
  return request('/payment-providers')
}

export function createProvider(
  name: string,
  instructionsTemplate: string,
): Promise<PaymentProvider> {
  return request('/payment-providers', {
    body: { name, instructions_template: instructionsTemplate },
  })
}

export function updateProvider(
  id: string,
  patch: Partial<Pick<PaymentProvider, 'name' | 'instructions_template' | 'enabled'>>,
): Promise<PaymentProvider> {
  return request(`/payment-providers/${id}`, { method: 'PUT', body: patch })
}

export interface ProviderAccount {
  id: string
  provider_id: string
  account_details: string
  instructions_override?: string | null
}

export function listProviderAccounts(managerId: string): Promise<{ items: ProviderAccount[] }> {
  return request(`/managers/${managerId}/provider-accounts`)
}

export function putProviderAccount(
  managerId: string,
  providerId: string,
  accountDetails: string,
  instructionsOverride?: string,
): Promise<ProviderAccount> {
  return request(`/managers/${managerId}/provider-accounts/${providerId}`, {
    method: 'PUT',
    body: {
      account_details: accountDetails,
      instructions_override: instructionsOverride || undefined,
    },
  })
}

export interface MethodSetting {
  method_key: string
  enabled: boolean
}

export function listMethodSettings(managerId: string): Promise<{ items: MethodSetting[] }> {
  return request(`/managers/${managerId}/method-settings`)
}

export function putMethodSetting(
  managerId: string,
  methodKey: string,
  enabled: boolean,
): Promise<MethodSetting> {
  return request(`/managers/${managerId}/method-settings`, {
    method: 'PUT',
    body: { method_key: methodKey, enabled },
  })
}

// --- Instance defaults (subscribers with no owning manager; admin-only) -----

export function listInstanceMethodSettings(): Promise<{ items: MethodSetting[] }> {
  return request('/instance/method-settings')
}

export function putInstanceMethodSetting(
  methodKey: string,
  enabled: boolean,
): Promise<MethodSetting> {
  return request('/instance/method-settings', {
    method: 'PUT',
    body: { method_key: methodKey, enabled },
  })
}

export function listInstanceProviderAccounts(): Promise<{ items: ProviderAccount[] }> {
  return request('/instance/provider-accounts')
}

export function putInstanceProviderAccount(
  providerId: string,
  accountDetails: string,
  instructionsOverride?: string,
): Promise<ProviderAccount> {
  return request(`/instance/provider-accounts/${providerId}`, {
    method: 'PUT',
    body: {
      account_details: accountDetails,
      instructions_override: instructionsOverride || undefined,
    },
  })
}
