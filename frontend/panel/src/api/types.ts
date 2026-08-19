/**
 * TypeScript mirrors of the Phase-2 backend read/write shapes (contract C7 in
 * docs/phases/phase-2-aaa-pipeline/00-phase.md). Kept in one file so a contract
 * drift shows up as a single diff. Nullable columns are `T | null` to match the
 * Go pointer fields (a JSON `null`, distinct from "field omitted").
 */

// --- subscribers (C7-D) ---------------------------------------------------

export type SubscriberStatus = 'active' | 'expired' | 'disabled'
export type MacLockMode = 'off' | 'learn' | 'fixed'
/**
 * FR-61: which service(s) a subscriber may use. 'hotspot' is a full account
 * with no PPPoE access (quota and session limit apply); 'dual' is v1's
 * allow_hotspot=true (FR-58); 'pppoe' its false, and the default.
 */
export type ServiceType = 'pppoe' | 'hotspot' | 'dual'

/** Every service type, in the order the panel offers them. */
export const SERVICE_TYPES: ServiceType[] = ['pppoe', 'hotspot', 'dual']

/**
 * FR-64: one place an account may authenticate. An EMPTY `nas_service_id` means
 * the whole NAS — every service instance on it. (The backend serializes the
 * whole-NAS case as `""`, not null.)
 *
 * An account carries a SET of these. The set being EMPTY means "any NAS" (the
 * v1 default that most accounts have), not "nowhere".
 */
export interface NasScope {
  nas_id: string
  nas_service_id: string
}

export interface Subscriber {
  id: string
  username: string
  name: string | null
  phone: string | null
  /** FR-85.1: nullable, unvalidated at the DB level (validated in Go), not a credential. */
  email: string | null
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
  /** FR-109: overrides the profile's address_list; null = inherit. */
  address_list_override: string | null
  price_override: number | null
  disabled_reason: string | null
  /** FR-61: which service(s) this account may use. Replaced v1's allow_hotspot. */
  service_type: ServiceType
  whatsapp_opt_in: boolean
  /** FR-64 scope: every NAS/service this account may authenticate on. [] = any NAS (the default). */
  nas_scopes: NasScope[]
  pending_profile_id: string | null
  /** false = deliberately passwordless hotspot login (item 13). */
  has_password: boolean
  created_at: string
  updated_at: string
}

/** Create/update body — every field but `username` is optional on update. */
export interface SubscriberWrite {
  username?: string
  password?: string
  name?: string | null
  phone?: string | null
  email?: string | null
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
  /** FR-109: overrides the profile's address_list; null = inherit. */
  address_list_override?: string | null
  price_override?: number | null
  disabled_reason?: string | null
  service_type?: ServiceType
  whatsapp_opt_in?: boolean
  /**
   * FR-64 scope. An empty array clears it back to "any NAS"; omitting the field
   * entirely leaves the existing set untouched.
   */
  nas_scopes?: NasScope[]
  /** true clears/omits the credential — the NAS then sends password="". */
  no_password?: boolean
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

/** This-cycle (resettable) usage — derived live, moves to zero on a reset. */
export interface CycleCounters {
  quota_used_bytes: number
  uptime_used_seconds: number
  daily_used_bytes: number
}

/**
 * Lifetime billing totals (v4-5, FR-134) — never written by a reset. Kept
 * visually and structurally distinct from CycleCounters so an operator never
 * has to guess which figure a reset would affect.
 */
export interface LifetimeCounters {
  bytes_down: number
  bytes_up: number
  session_seconds: number
  first_seen_at: string | null
  last_seen_at: string | null
}

export interface CounterSummary {
  cycle: CycleCounters
  lifetime: LifetimeCounters
}

/**
 * Per-row list-depth data (v4-5, FR-135): daily/quota/uptime progress + days
 * remaining + the plan-swap badge, batched onto GET /api/v1/subscribers
 * keyed by subscriber id (see the `limits` sibling field on the list
 * response, not embedded per-item, so the same shape and derivation as
 * CounterSummary/CycleCounters is reused rather than duplicated per item).
 */
export interface PlanSwapBadge {
  active: boolean
  from_profile_id?: string
  to_profile_id?: string
  to_profile_name?: string
}

export interface LimitProgress {
  subscriber_id: string
  quota_mode: string
  quota_used_bytes: number
  quota_used_down: number
  quota_used_up: number
  quota_total_bytes: number
  quota_down_bytes: number
  quota_up_bytes: number
  uptime_used_seconds: number
  uptime_limit_seconds: number
  daily_used_bytes: number
  daily_limit_bytes: number
  days_remaining: number | null
  plan_swap: PlanSwapBadge
}

export interface SubscriberDetail {
  subscriber: Subscriber
  profile: ProfileSummary | null
  owner: OwnerSummary | null
  live: LiveFlag
  overrides: OverrideBadges
  links: Record<string, string>
  counters: CounterSummary
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
  /** FR-61. Must mirror the list's own service_type filter — see UserListPage. */
  service_type?: string
}

export type BulkAction =
  | 'enable'
  | 'disable'
  | 'change_profile'
  | 'extend_expiry'
  | 'move_owner'
  | 'set_service_type'
  | 'delete'
  | 'export'

export interface BulkRequest {
  filter: BulkFilter
  /**
   * An explicit selection. When non-empty it REPLACES `filter` — the operator
   * ticked these rows and means these rows. The manager scope still applies, so
   * this cannot reach a subscriber the caller couldn't otherwise see.
   */
  subscriber_ids?: string[]
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
// v4-4 (FR-130): the unified exhaustion action set — expiry/quota widen
// additively (existing values are an exact subset), uptime/daily are new.
export type ExhaustAction = 'block' | 'throttle' | 'expired_pool' | 'switch_plan'
export type ExpiryBehavior = ExhaustAction
export type QuotaBehavior = ExhaustAction

/** FR-131: a profile's time-of-day rate override. days_mask bit N = day N
 * (0=Sunday..6=Saturday); 0 = every day. */
export interface SpeedWindow {
  id: string
  profile_id: string
  label: string
  days_mask: number
  start_min: number
  end_min: number
  rate_down_kbps: number
  rate_up_kbps: number
  enabled: boolean
  created_at: string
}

export interface SpeedWindowWrite {
  label?: string
  days_mask: number
  start_min: number
  end_min: number
  rate_down_kbps: number
  rate_up_kbps: number
  enabled?: boolean
}

export interface Profile {
  id: string
  name: string
  price: number
  currency: string
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
  /** FR-109: named MikroTik address-list applied at a normal login. */
  address_list: string | null
  expiry_behavior: ExpiryBehavior
  quota_behavior: QuotaBehavior
  hotspot_rate_down_kbps: number | null
  hotspot_rate_up_kbps: number | null
  /** FR-64 scope inherited by this profile's subscribers; [] = any NAS. */
  nas_scopes: NasScope[]
  archived: boolean

  // --- v4-4 Limits engine (FR-128–131) -----------------------------------
  uptime_limit_seconds: number | null
  daily_limit_bytes: number | null
  /** Minutes since local (server timezone) midnight. */
  daily_reset_min: number
  /** Meaning depends on the paired action: a rate string for throttle, a
   * target profile id for switch_plan, an optional pool-name override for
   * expired_pool ("" = the global expired pool). */
  expiry_exhaust_target: string
  quota_exhaust_target: string
  uptime_exhaust_action: ExhaustAction
  uptime_exhaust_target: string
  daily_exhaust_action: ExhaustAction
  daily_exhaust_target: string
  /** FR-132: subscriber/window counts, list/get only. */
  subscriber_count: number
  speed_window_count: number
  auto_renew_default: boolean

  created_at: string
  updated_at: string
}

export interface ProfileWrite {
  name: string
  price: number
  currency?: string
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
  /** FR-109: named MikroTik address-list applied at a normal login. */
  address_list?: string | null
  expiry_behavior: ExpiryBehavior
  quota_behavior: QuotaBehavior
  hotspot_rate_down_kbps?: number | null
  hotspot_rate_up_kbps?: number | null
  /** FR-64 scope; an empty array means "any NAS". */
  nas_scopes?: NasScope[]
  archived?: boolean

  // --- v4-4 Limits engine (FR-128–131) -----------------------------------
  uptime_limit_seconds?: number | null
  daily_limit_bytes?: number | null
  daily_reset_min?: number
  expiry_exhaust_target?: string
  quota_exhaust_target?: string
  uptime_exhaust_action?: ExhaustAction
  uptime_exhaust_target?: string
  daily_exhaust_action?: ExhaustAction
  daily_exhaust_target?: string
  auto_renew_default?: boolean
}

// --- profile bulk (FR-111.2) ----------------------------------------------

export interface ProfileBulkFilter {
  q?: string
  archived?: string // ""|"any" = both; "yes"; "no"
  quota_mode?: string
  expiry_behavior?: string
  pool_id?: string
}

export type ProfileBulkAction =
  | 'archive'
  | 'unarchive'
  | 'change_pool'
  | 'set_address_list'
  | 'set_expiry_behavior'
  | 'set_quota_behavior'
  | 'delete'
  | 'export'

export interface ProfileBulkRequest {
  filter: ProfileBulkFilter
  /** Explicit selection; non-empty REPLACES the filter (profiles are instance-global — no scoping). */
  profile_ids?: string[]
  action: ProfileBulkAction
  params?: Record<string, unknown>
}

export interface ProfileBulkFailure {
  profile_id: string
  name: string
  error: string
}

export interface ProfileBulkJob {
  id: string
  action: string
  status: 'running' | 'completed'
  total: number
  done: number
  succeeded: number
  failed: number
  failures: ProfileBulkFailure[]
  started_at: string
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

/** The kind of one service instance a NAS runs (FR-62). */
export type NasType = 'pppoe' | 'hotspot'

/** router = discovered/adopted (read-only until adopted); system = HikRAD-provisioned/owned (v2 phase 2, FR-67). */
export type NasServiceManagementMode = 'router' | 'system'

/** One service instance on a NAS (FR-62 / C3). */
export interface NasService {
  id: string
  service: NasType
  /** Zone / SSID / friendly name. */
  label: string
  interface_note: string
  ip_pool_id: string | null
  ip_pool_name: string
  /** The router's own name for this instance (hotspot server / PPPoE service-name). */
  ros_server_name: string
  enabled: boolean
  live_sessions: number
  management_mode: NasServiceManagementMode
  /** v4-7 FR-145 extended parameters — HikRAD-side metadata, no router write. */
  interim_interval_secs?: number | null
  hotspot_user_profile?: string
  dhcp_range?: string
  ipv6_enabled?: boolean
  ipv6_prefix_len?: number
  ipv6_pool_name?: string
}

/**
 * A service instance read off the router (FR-62.6). `matched_service_id` is set
 * when this NAS already has the instance, so a re-run offers no duplicates.
 */
export interface DiscoveredService {
  service: NasType
  ros_server_name: string
  label: string
  interface_note: string
  /** The pool name the ROUTER has for this hotspot — what Framed-Pool must match. */
  router_pool_name: string
  enabled: boolean
  matched_service_id: string
}

/**
 * FR-62.7: router-side config that breaks HikRAD logins even when HikRAD is
 * behaving correctly — the "RADIUS says accept and the user still can't get on"
 * class. `code` is localized by the panel; `detail` carries the router's own
 * strings, `fix` the exact command that resolves it.
 */
export interface NasHealthFinding {
  code: string
  detail: string
  fix: string
}

/** One service instance in a NAS write body; id identifies an existing row. */
export interface NasServiceWrite {
  id?: string
  service: NasType
  label?: string
  interface_note?: string
  ip_pool_id?: string | null
  ros_server_name?: string
  enabled?: boolean
}

export interface Nas {
  id: string
  name: string
  ip: string
  /** Every service instance this NAS runs (FR-62); replaced v1's single `type`. */
  services: NasService[]
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
  /**
   * The whole truth for this NAS's services: an omitted row is deleted, and at
   * least one is required.
   */
  services: NasServiceWrite[]
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

/** A conflict's resolution choice (v2 phase 2, FR-66.2). Default/unset = abort. */
export type AutoSetupResolution = 'update' | 'keep'

export interface AutoSetupConflict {
  path: string
  existing: string
  reason: string
  /** Stable id for this conflict across preview calls — key into the resolutions map. */
  key: string
  /** Whether "update" has a computable target sentence at all. */
  resolvable: boolean
  /** The exact command an "update" resolution would run, shown for operator approval. */
  update_command?: string
}

export interface AutoSetupPreview {
  items: AutoSetupPlanItem[]
  conflicts: AutoSetupConflict[]
  preview_hash: string
  ros_version: string
}

/** FR-66.1 values-form overrides — every field optional (omitted = HikRAD's default). */
export interface AutoSetupValues {
  radius_server?: string
  src_address?: string
  coa_port?: number
  interim_secs?: number
  walled_garden?: string[]
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

// --- Config inspection (v2 phase 2, FR-65) ---------------------------------

export interface RadiusEntryConfig {
  address: string
  service: string
  comment: string
  src_address: string
  secret_present: boolean
}

export interface HotspotProfileConfig {
  name: string
  use_radius: boolean
  interim_update_secs: number
}

export interface NasConfig {
  nas_id: string
  ros_version: string
  board_name: string
  identity: string
  radius: RadiusEntryConfig[]
  radius_incoming: { accept: boolean; port: number }
  ppp_aaa: { use_radius: boolean; accounting: boolean; interim_update_secs: number }
  hotspot_profiles: HotspotProfileConfig[]
  walled_garden: string[]
}

// --- Server management (v2 phase 2, FR-67) ---------------------------------

/** Create-or-edit body for one server instance; service_id set = edit. */
export interface ServiceProvisionRequest {
  service_id?: string
  service: NasType
  label?: string
  interface?: string
  ros_server_name: string
  ip_pool_id?: string | null
  local_address?: string
  address_range?: string
  values?: AutoSetupValues
}

export interface ServiceApplyResult {
  results: AutoSetupApplyResultItem[]
  all_ok: boolean
  service: NasService
}

/** Live router-side view of one service instance, both management modes. */
export interface ServiceRouterConfig {
  service: NasType
  ros_server_name: string
  label: string
  interface_note: string
  router_pool_name: string
  enabled: boolean
  matched_service_id: string
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

/**
 * FR-108 pool ownership (Decision 48): 'system' = HikRAD owns it (editable,
 * deletable, pushable to a router); 'router' = adopted from a router, a
 * read-only reference HikRAD never writes back.
 */
export type PoolManagementMode = 'system' | 'router'

export interface Pool {
  id: string
  name: string
  ranges: string[]
  purpose: PoolPurpose
  management_mode: PoolManagementMode
  /** The router's raw ranges string for an adopted pool (not valid inet, kept verbatim). */
  router_ranges?: string
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
  /**
   * FR-62: WHICH of the NAS's service instances this session is on. `service`
   * alone says "hotspot", which on a router running three zones doesn't tell an
   * operator where the user actually is. Empty when the session couldn't be
   * attributed to an instance (recorded anyway — a session is never dropped).
   */
  nas_service_id?: string
  service_name?: string
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

/**
 * CoA disconnect enqueue result (v4-8, FR-150/151). Disconnect is no longer
 * dispatched inline — this is 202 with the coa_outbox row id; the actual
 * outcome (ack/nak/timeout, plus the hybrid fallback if it comes to that)
 * shows up on the CoA Outbox screen, not synchronously here.
 */
export interface DisconnectResult {
  outbox_id: string
  state: string
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

// --- Router manager I (v4-6, FR-139-143) ----------------------------------

/** One row of the FR-139 monitor grid — a NAS or a generic monitored device. */
export interface DeviceMonitorRow {
  id: string
  /** "nas" | "monitor" — the NAS-vs-monitor tag. */
  kind: 'nas' | 'monitor'
  name: string
  ip: string
  enabled: boolean
  /** Whether the device worker has ever produced a sample for this device. */
  sampled: boolean
  has_api_credentials: boolean
  reachable?: boolean
  ros_version?: string
  cpu_percent?: number
  cpu_cores?: number
  mem_percent?: number
  session_count?: number
  uptime_secs?: number
  last_sample_at?: string
}

/** Ownership badge vocabulary (router-ownership principle). */
export type DeviceOwnership = 'system' | 'router' | 'mixed'

/** Frozen FR-140 shape every /nas/{id}/device/{tab} read returns. */
export interface DeviceTabEnvelope<T> {
  data: T | null
  read_at: string | null
  stale: boolean
  ownership: DeviceOwnership
  no_api_credentials: boolean
}

export interface DeviceHistoryPoint {
  time: string
  reachable: boolean
  cpu_percent: number
  mem_percent: number
  session_count: number
}

export interface DeviceOverview {
  identity: string
  board_name: string
  ros_version: string
  serial_number: string
  reachable: boolean
  cpu_percent: number
  cpu_cores: number
  mem_percent: number
  session_count: number
  uptime_secs: number
  history: DeviceHistoryPoint[]
}

export interface DeviceInterface {
  name: string
  type: string
  running: boolean
  disabled: boolean
  mac_address: string
  mtu: number
  comment: string
  rx_bps: number
  tx_bps: number
  ownership: DeviceOwnership
}

export interface DeviceTrafficPoint {
  time: string
  rx_bps: number
  tx_bps: number
}

export interface DeviceInterfaces {
  interfaces: DeviceInterface[]
  selected_iface?: string
  traffic_history?: DeviceTrafficPoint[]
}

export interface DeviceGeneral {
  identity: string
  time_zone: string
  time: string
  date: string
  ntp_enabled: boolean
  ntp_mode: string
  ntp_servers: string[] | null
}

export interface DeviceBridgePort {
  interface: string
  pvid: number
}

export interface DeviceBridge {
  name: string
  comment: string
  ports: DeviceBridgePort[] | null
}

export interface DeviceVlan {
  name: string
  vlan_id: number
  interface: string
  comment: string
}

/** Bridge/VLAN cache rows carry a per-item ownership badge (always "router" this phase). */
export interface DeviceOwnedItem<T> {
  item: T
  ownership: DeviceOwnership
}

export interface DeviceIpAddress {
  address: string
  interface: string
  comment: string
  disabled: boolean
}

export interface DeviceDhcpClient {
  interface: string
  status: string
  address: string
  gateway: string
}

export interface DeviceDhcpServer {
  name: string
  interface: string
  pool_name: string
  disabled: boolean
}

export interface DeviceDhcpLease {
  address: string
  mac: string
  hostname: string
  server: string
  status: string
  expires_in: string
}

export interface DeviceIpTab {
  snapshot: {
    addresses: DeviceIpAddress[] | null
    dhcp_clients: DeviceDhcpClient[] | null
    dhcp_servers: DeviceDhcpServer[] | null
    leases: DeviceDhcpLease[] | null
  }
  /** DHCP server name -> "system" | "router" (FR-108 pool-ownership badge). */
  dhcp_server_ownership: Record<string, DeviceOwnership> | null
}

export interface DeviceDnsStaticEntry {
  name: string
  address: string
  comment: string
}

export interface DeviceDns {
  servers: string[] | null
  allow_remote_requests: boolean
  cache_size: number
  cache_used: number
  static: DeviceDnsStaticEntry[] | null
}

export interface DevicePingResult {
  sent: number
  received: number
  packet_loss_percent: number
  min_rtt_ms: number
  avg_rtt_ms: number
  max_rtt_ms: number
}

// --- v4-7 Router manager II (FR-144-149) ------------------------------------

/**
 * The wizard's whole accumulating form (FR-144). One object, PUT to
 * wizard/state on every step transition and POSTed verbatim to wizard/plan —
 * so "what the review step previews" and "what was saved for resume" can
 * never drift apart.
 */
export interface WizardFormData {
  bridge?: WizardBridgeInput | null
  dns?: WizardDnsInput | null
  ntp?: WizardNtpInput | null
  wan?: WizardWanInput[]
  values?: AutoSetupValues
  services?: WizardServiceInput[]
  firewall?: WizardFirewallInput | null
  ipv6?: WizardIpv6Input[]
}

export interface WizardBridgeInput {
  name: string
  ports: string[]
}

export interface WizardDnsInput {
  servers: string[]
  allow_remote_requests: boolean
  cache_size: number
}

export interface WizardNtpInput {
  servers: string[]
}

export type WanMode = 'static' | 'dhcp' | 'pppoe'

export interface WizardWanInput {
  interface: string
  label: string
  mode: WanMode
  address?: string
  gateway?: string
  pppoe_user?: string
  pppoe_password?: string
}

export interface WizardServiceInput {
  service_id?: string
  service: NasType
  label: string
  interface: string
  ros_server_name: string
  ip_pool_id?: string | null
  local_address?: string
  address_range?: string
}

export interface WizardExpiredPoolInput {
  pool_name: string
  ranges: string[]
  redirect_url: string
}

export interface WizardFirewallInput {
  nat_out_interfaces: string[]
  expired_pool?: WizardExpiredPoolInput | null
}

/** FR-149 opt-in dual-stack, one entry per service instance that opts in. */
export interface WizardIpv6Input {
  enabled: boolean
  ros_server_name: string
  prefix: string
  prefix_len: number
  pool_name: string
}

/** The plan/apply body; apply adds preview_hash. `resolutions` absent = abort. */
export interface WizardPlanRequest extends WizardFormData {
  values: AutoSetupValues
  resolutions?: Record<string, AutoSetupResolution>
}

export interface WizardState {
  step: number
  form_data: WizardFormData
  completed: boolean
}

/** One step's detect verdict — `already_correct` is what lets a step be skipped. */
export interface WizardStepStatus {
  already_correct: boolean
  summary: string
}

export interface WizardDetection {
  bridge: WizardStepStatus
  dns: WizardStepStatus
  ntp: WizardStepStatus
  wan: WizardStepStatus
  radius: WizardStepStatus
  services: WizardStepStatus
}

export interface WizardApplyResult {
  results: AutoSetupApplyResultItem[]
  all_ok: boolean
}

/** FR-145 Internet tab row. `management_mode: 'router'` = read-only reference. */
export interface WanUplink {
  interface: string
  mode: WanMode
  address: string
  gateway: string
  status?: string
  comment?: string
  management_mode: NasServiceManagementMode
}

export interface WanAdoptResult {
  adopted: number
  total: number
}

/** FR-146 firewall rule. `ownership: 'router'` rules are not editable by HikRAD. */
export interface FirewallRule {
  id: string
  chain: string
  table: FirewallTable
  action: string
  src_address?: string
  dst_address?: string
  out_interface?: string
  in_interface?: string
  protocol?: string
  dst_port?: string
  comment?: string
  disabled: boolean
  ownership: 'system' | 'router'
}

export type FirewallTable = 'filter' | 'nat'

export interface FirewallChangeRequest {
  rule_id?: string
  table: FirewallTable
  fields: Record<string, string>
  description?: string
}

/** Present on an apply that has something to revert — drives the countdown banner. */
export interface FirewallRevertInfo {
  snapshot_id: string
  deadline: string
}

export interface FirewallApplyResult {
  results: AutoSetupApplyResultItem[]
  all_ok: boolean
  revert?: FirewallRevertInfo
}

export interface FirewallPendingChange {
  id: string
  description: string
  deadline: string
  created_at: string
}

/** FR-147 Update tab read. */
export interface RouterUpdateInfo {
  channel: string
  installed_version: string
  latest_version: string
  update_available: boolean
  packages: RouterPackage[] | null
}

export interface RouterPackage {
  name: string
  version: string
  enabled: boolean
}

/** FR-145 metadata-only per-service parameters (no router write). */
export interface ServiceParamsRequest {
  interim_interval_secs?: number | null
  hotspot_user_profile: string
  dhcp_range: string
  ipv6_enabled: boolean
  ipv6_prefix_len: number
  ipv6_pool_name: string
}

/** FR-148 hotspot login page design + push state. */
export type HotspotPageMode = 'router_default' | 'branded_template' | 'custom'

export type HotspotPageStaleReason = 'never_pushed' | 'unpushed_edit' | 'ros_upgraded_since_push'

export interface HotspotPage {
  mode: HotspotPageMode
  design_version: number
  pushed_version: number | null
  pushed_at: string | null
  pushed_ros_version?: string
  stale: boolean
  stale_reason?: HotspotPageStaleReason
  custom_html?: string
}

export interface HotspotPagePushResult {
  status: string
  design_version: number
  ros_version: string
}
