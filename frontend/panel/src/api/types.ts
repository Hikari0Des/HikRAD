/**
 * TypeScript mirrors of the Phase-2 backend read/write shapes (contract C7 in
 * docs/phases/phase-2-aaa-pipeline/00-phase.md). Kept in one file so a contract
 * drift shows up as a single diff. Nullable columns are `T | null` to match the
 * Go pointer fields (a JSON `null`, distinct from "field omitted").
 */

// --- subscribers (C7-D) ---------------------------------------------------

export type SubscriberStatus = 'active' | 'expired' | 'disabled'
export type MacLockMode = 'off' | 'learn' | 'fixed'

export interface Subscriber {
  id: string
  username: string
  name: string | null
  phone: string | null
  address: string | null
  notes: string | null
  status: SubscriberStatus
  profile_id: string | null
  owner_manager_id: string | null
  expires_at: string | null
  mac_lock_mode: MacLockMode
  learned_mac: string | null
  static_ip: string | null
  session_limit_override: number | null
  rate_override: string | null
  price_override: number | null
  disabled_reason: string | null
  allow_hotspot: boolean
  whatsapp_opt_in: boolean
  pending_profile_id: string | null
  created_at: string
  updated_at: string
}

/** Create/update body — every field but `username` is optional on update. */
export interface SubscriberWrite {
  username?: string
  password?: string
  name?: string | null
  phone?: string | null
  address?: string | null
  notes?: string | null
  status?: SubscriberStatus
  profile_id?: string | null
  owner_manager_id?: string | null
  expires_at?: string | null
  mac_lock_mode?: MacLockMode
  static_ip?: string | null
  session_limit_override?: number | null
  rate_override?: string | null
  price_override?: number | null
  disabled_reason?: string | null
  allow_hotspot?: boolean
  whatsapp_opt_in?: boolean
}

export interface ProfileSummary {
  id: string
  name: string
  rate_down_kbps: number
  rate_up_kbps: number
  duration_days: number
  quota_mode: string
  expiry_behavior: string
  quota_behavior: string
  archived: boolean
  pool_name: string | null
}

export interface OwnerSummary {
  id: string
  username: string
}

export interface OverrideBadges {
  rate: boolean
  price: boolean
  session_limit: boolean
  static_ip: boolean
}

export interface LiveFlag {
  online: boolean
  sessions: number
}

export interface SubscriberDetail {
  subscriber: Subscriber
  profile: ProfileSummary | null
  owner: OwnerSummary | null
  live: LiveFlag
  overrides: OverrideBadges
  links: Record<string, string>
}

/** update returns the subscriber plus an optional CoA-disconnect offer (FR-1.2). */
export interface SubscriberUpdateResult {
  subscriber: Subscriber
  offer_disconnect?: boolean
}

// --- bulk (C7-D) ----------------------------------------------------------

export interface BulkFilter {
  status?: string
  profile_id?: string
  owner_manager_id?: string
  q?: string
  expiring_before?: string | null
}

export type BulkAction =
  | 'enable'
  | 'disable'
  | 'change_profile'
  | 'extend_expiry'
  | 'move_owner'
  | 'set_allow_hotspot'
  | 'export'

export interface BulkRequest {
  filter: BulkFilter
  action: BulkAction
  params?: Record<string, unknown>
}

export interface BulkFailure {
  subscriber_id: string
  username: string
  error: string
}

export interface BulkJob {
  id: string
  action: string
  status: 'running' | 'completed'
  total: number
  done: number
  succeeded: number
  failed: number
  failures: BulkFailure[]
  started_at: string
}

// --- search (C7-D) --------------------------------------------------------

export interface SearchHit {
  type: 'subscriber'
  id: string
  username: string
  name: string | null
  phone: string | null
  status: SubscriberStatus
}

// --- profiles (C7-D) ------------------------------------------------------

export type QuotaMode = 'unlimited' | 'total' | 'split'
export type ExpiryBehavior = 'block' | 'expired_pool'
export type QuotaBehavior = 'block' | 'throttle' | 'expired_pool'

export interface Profile {
  id: string
  name: string
  price_iqd: number
  duration_days: number
  rate_down_kbps: number
  rate_up_kbps: number
  pool_id: string | null
  session_limit_default: number
  quota_mode: QuotaMode
  quota_total_bytes: number | null
  quota_down_bytes: number | null
  quota_up_bytes: number | null
  throttle_rate: string | null
  expiry_behavior: ExpiryBehavior
  quota_behavior: QuotaBehavior
  hotspot_rate_down_kbps: number | null
  hotspot_rate_up_kbps: number | null
  archived: boolean
  created_at: string
  updated_at: string
}

export interface ProfileWrite {
  name: string
  price_iqd: number
  duration_days: number
  rate_down_kbps: number
  rate_up_kbps: number
  pool_id?: string | null
  session_limit_default: number
  quota_mode: QuotaMode
  quota_total_bytes?: number | null
  quota_down_bytes?: number | null
  quota_up_bytes?: number | null
  throttle_rate?: string | null
  expiry_behavior: ExpiryBehavior
  quota_behavior: QuotaBehavior
  hotspot_rate_down_kbps?: number | null
  hotspot_rate_up_kbps?: number | null
  archived?: boolean
}

export interface OnlineRef {
  subscriber_id: string
  username: string
  nas_id: string
  acct_session_id: string
  framed_ip: string
}

export interface ProfileUpdateResult {
  profile: Profile
  applied: 'now' | 'next_renewal'
  online_affected: OnlineRef[]
}

// --- NAS (C7-B) -----------------------------------------------------------

export type NasType = 'pppoe' | 'hotspot'

export interface Nas {
  id: string
  name: string
  ip: string
  type: NasType
  vendor: string
  coa_port: number
  has_snmp: boolean
  ros_version: string | null
  location: string
  enabled: boolean
  /** FR-56.2 RouterOS API auto-setup credential slice; password never round-trips. */
  api_port: number
  api_user: string
  has_api_creds: boolean
  created_at: string
  updated_at: string
}

export interface NasWrite {
  name: string
  ip: string
  secret?: string
  type?: NasType
  vendor?: string
  coa_port?: number
  snmp_community?: string
  ros_version?: string | null
  location?: string
  enabled?: boolean
  api_port?: number
  api_user?: string
  api_password?: string
}

// --- NAS auto-setup (C6, FR-56.2-56.4) -------------------------------------

export interface AutoSetupPlanItem {
  action: 'add' | 'set'
  path: string
  command: string
  current_state: string
}

export interface AutoSetupConflict {
  path: string
  existing: string
  reason: string
}

export interface AutoSetupPreview {
  items: AutoSetupPlanItem[]
  conflicts: AutoSetupConflict[]
  preview_hash: string
  ros_version: string
}

export interface AutoSetupApplyResultItem {
  path: string
  command: string
  ok: boolean
  error?: string
}

export interface AutoSetupApplyResult {
  results: AutoSetupApplyResultItem[]
  all_ok: boolean
  seen: { last_auth_at: string | null; last_acct_at: string | null; seen: boolean }
}

export interface NasStatus {
  id: string
  ip: string
  last_auth_at: string | null
  last_acct_at: string | null
  seen: boolean
}

export interface NasSnippet {
  nas_id: string
  ros_version: string
  type: string
  snippet: string
}

export interface DiscoveredNas {
  ip: string
  identity: string
  ros_version: string
  mac: string
  already_registered: boolean
}

// --- IP pools (C7-B) ------------------------------------------------------

export type PoolPurpose = 'active' | 'expired' | 'static'

export interface Pool {
  id: string
  name: string
  ranges: string[]
  purpose: PoolPurpose
  size: number
  used: number
  util_percent: number
  exhausted: boolean
  created_at: string
  updated_at: string
}

export interface PoolWrite {
  name: string
  ranges: string[]
  purpose?: PoolPurpose
}

// --- live / sessions / usage (C6, C7-C) -----------------------------------

/** The Redis live-session JSON forwarded verbatim by the SSE feed (C6). */
export interface LiveSession {
  username: string
  subscriber_id: string
  nas_id: string
  acct_session_id: string
  ip: string
  mac: string
  started_at: string
  last_interim_at: string
  bytes_in: number
  bytes_out: number
  rate_down_bps: number
  rate_up_bps: number
  stale: boolean
  service: 'pppoe' | 'hotspot'
}

export interface SessionHistory {
  id: string
  nas_id: string
  acct_session_id: string
  subscriber_id: string
  username: string
  ip: string
  mac: string
  started_at: string | null
  stopped_at: string | null
  last_interim_at: string | null
  terminate_cause: string
  bytes_in: number
  bytes_out: number
  stale: boolean
  reaped: boolean
  service: 'pppoe' | 'hotspot'
}

export interface UsagePoint {
  t: string
  down: number
  up: number
}

/** CoA disconnect outcome (C5). 200 = ack; 502 = NAK/timeout with `error`. */
export interface DisconnectResult {
  outcome: string
  error?: string
}

// --- audit log (A) --------------------------------------------------------

export interface AuditEntry {
  id: number
  actor_id: string | null
  action: string
  entity_type: string
  entity_id: string
  before: unknown
  after: unknown
  ip: string
  ua: string
  at: string
}
