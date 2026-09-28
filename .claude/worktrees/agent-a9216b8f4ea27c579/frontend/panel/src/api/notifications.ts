/**
 * v4-10 (FR-160-163): the Notification Center. Response shapes mirror
 * internal/notify exactly.
 */
import { listPage, request, type Page, type PageParams } from './client'

export type NotifyChannel = 'inapp' | 'telegram' | 'email' | 'whatsapp' | 'sms'

export interface OutboxCounters {
  queued: number
  sending: number
  sent_today: number
  failed: number
  skipped: number
  total: number
}

export interface ChannelStatus {
  channel: NotifyChannel
  configured: boolean
}

export interface OverviewResponse {
  counters: OutboxCounters
  channels: ChannelStatus[]
}

export function getOverview(): Promise<OverviewResponse> {
  return request('/notifications/overview')
}

export function listChannelStatuses(): Promise<ChannelStatus[]> {
  return request('/notifications/channels')
}

export interface NotificationTemplate {
  ID: string
  EventKey: string
  Locale: string
  Version: number
  Subject: string
  Body: string
  IsDefault: boolean
  CreatedAt: string
  CreatedBy: string
}

export function listTemplates(): Promise<{ items: NotificationTemplate[] }> {
  return request('/notifications/templates')
}

export function getTemplateHistory(
  event: string,
  locale: string,
): Promise<{ items: NotificationTemplate[] }> {
  return request(`/notifications/templates/${event}/${locale}/history`)
}

export function putTemplate(
  event: string,
  locale: string,
  body: { subject: string; body: string },
): Promise<NotificationTemplate> {
  return request(`/notifications/templates/${event}/${locale}`, { method: 'PUT', body })
}

export interface NotificationRule {
  ID: string
  EventKey: string
  Name: string
  Enabled: boolean
  Channels: NotifyChannel[]
  DaysBefore: number | null
  IgnoreOptOut: boolean
  BatchSize: number
  TickIntervalSeconds: number
}

export function listRules(): Promise<{ items: NotificationRule[] }> {
  return request('/notifications/rules')
}

export interface RuleUpdate {
  enabled?: boolean
  channels?: NotifyChannel[]
  days_before?: number | null
  ignore_opt_out?: boolean
  batch_size?: number
  tick_interval_seconds?: number
}

export function putRule(event: string, body: RuleUpdate): Promise<NotificationRule> {
  return request(`/notifications/rules/${event}`, { method: 'PUT', body })
}

export function broadcast(body: {
  subscriber_ids: string[]
  channel: NotifyChannel
  subject: string
  body: string
}): Promise<{ queued: number; ids: string[] }> {
  return request('/notifications/broadcast', { method: 'POST', body })
}

export interface OutboxItem {
  id: string
  source: string
  source_id?: string
  event: string
  subscriber_id?: string
  manager_id?: string
  address: string
  channel: NotifyChannel
  locale: string
  template_key?: string
  template_version?: number
  subject?: string
  body: string
  ignore_opt_out: boolean
  state: 'queued' | 'sending' | 'sent' | 'failed' | 'skipped'
  attempt_count: number
  last_error?: string
  skip_reason?: string
  created_at: string
  updated_at: string
  sent_at?: string
}

export function listOutbox(
  params: PageParams & { state?: string; channel?: string; event?: string; search?: string },
): Promise<Page<OutboxItem>> {
  const { state, channel, event, search, ...page } = params
  return listPage('/notifications/outbox', page, { state, channel, event, search })
}

export function getOutboxCounters(): Promise<OutboxCounters> {
  return request('/notifications/outbox/counters')
}

export function getOutboxItem(id: string): Promise<OutboxItem> {
  return request(`/notifications/outbox/${id}`)
}

export function retryOutbox(id: string): Promise<{ id: string; state: string }> {
  return request(`/notifications/outbox/${id}/retry`, { method: 'POST' })
}

export function cancelOutbox(id: string): Promise<{ id: string; state: string }> {
  return request(`/notifications/outbox/${id}/cancel`, { method: 'POST' })
}
