// Package vendor is the RADIUS vendor-adapter boundary (FR-17). The policy
// engine and CoA service in the parent radius package speak only abstract
// intents (rate_limit, address_pool, session_timeout, redirect_expired,
// static_ip); an adapter here maps those onto concrete vendor VSAs and
// renders vendor NAS config. No vendor attribute name (e.g. Mikrotik-*)
// appears outside this package and the FreeRADIUS dictionaries/templates —
// scripts/lint-vendor-isolation enforces it, and radius depends on this
// package one-directionally (this package imports neither radius nor any
// domain code) so the core stays vendor-neutral with no import cycle.
package vendor

import "layeh.com/radius"

// Intent names — the abstract reply vocabulary. Kept as plain strings (not a
// radius.Intent import) so this package remains a dependency-free leaf; the
// radius package's Intent constants carry the same literals and a test there
// asserts they stay in sync.
const (
	IntentRateLimit       = "rate_limit"
	IntentAddressPool     = "address_pool"
	IntentSessionTimeout  = "session_timeout"
	IntentRedirectExpired = "redirect_expired"
	IntentStaticIP        = "static_ip"
	// IntentAddressList places a NORMAL login's IP into a named address-list
	// (FR-109). It maps to the same MikroTik VSA as IntentRedirectExpired
	// (Mikrotik-Address-List), but the two are emitted in mutually-exclusive
	// outcome modes (normal vs expired-redirect), so a single accept never
	// carries both.
	IntentAddressList = "address_list"
	// IntentDelegatedPrefix (v4-7, FR-149) carries an IPv6 prefix (CIDR, e.g.
	// "2001:db8:1234::/64") to delegate to the subscriber's CPE via DHCPv6-PD.
	// Standard RADIUS (Framed-IPv6-Prefix / Delegated-IPv6-Prefix), not a
	// vendor VSA, but mapped here like every other intent so no RADIUS
	// attribute name appears outside the adapter (FR-17.1).
	IntentDelegatedPrefix = "delegated_prefix"
)

// Attr is one abstract reply intent and its value.
type Attr struct {
	Intent string
	Value  string
}

// RateSpec is the abstract, vendor-neutral description of a rate limit with
// optional burst and time-of-day boost segments (FR-11). All rate fields are
// "rx/tx" pair strings (e.g. "10M/10M"); the adapter renders them into the
// concrete vendor rate string. Burst requires the full triple
// (BurstRate+BurstThreshold+BurstTime) to be meaningful; Priority/MinRate are
// only positionally valid once burst is present.
type RateSpec struct {
	Rate           string // base "rx/tx", required
	BurstRate      string // "rx/tx"
	BurstThreshold string // "rx/tx"
	BurstTime      string // "rx/tx" seconds
	Priority       string // single 1..8 (MikroTik queue priority)
	MinRate        string // "rx/tx" committed information rate
}

// ServiceQuery is one Access-Request's identifying attributes, as forwarded by
// the FreeRADIUS bridge. Service is the bridge's coarse pppoe|hotspot guess;
// the rest are raw RADIUS attribute VALUES whose vendor-specific *meaning*
// (how a MikroTik encodes a hotspot server name into Called-Station-Id, say) is
// interpreted only by an adapter in this package — that is the FR-17 boundary.
type ServiceQuery struct {
	// Service is the bridge's coarse kind: "pppoe" | "hotspot", or **EMPTY when
	// the kind is genuinely unknown**.
	//
	// Empty is not the same as "pppoe", and conflating the two was a real bug: a
	// MikroTik sends Service-Type=Login-User on a hotspot Access-Request but
	// omits it from Accounting-Requests, so accounting's guess defaulted to
	// pppoe. An adapter that FILTERS by that guess then finds no candidates on a
	// hotspot-only NAS and gives up before it ever looks at the attributes that
	// actually identify the instance — filing every hotspot session as pppoe
	// (2026-07-16, docs/ops/known-issues.md).
	//
	// So: a non-empty Service is a claim the caller stands behind and the adapter
	// may filter on; an empty one means "identify this from the attributes
	// alone", and the resolved instance's own service is the answer.
	Service         string
	CalledStationID string
	NASPortType     string
	NASPortID       string
}

// ServiceInstance is the subset of a nas_services row an adapter needs to match
// a request against (C7). ID is opaque to the adapter.
type ServiceInstance struct {
	ID            string
	Service       string // pppoe | hotspot
	ROSServerName string
}

// ServiceSnippet is one service instance the snippet must configure (C8).
type ServiceSnippet struct {
	Service       string // "pppoe" | "hotspot"
	Label         string // zone / SSID / friendly name, for comments
	ROSServerName string // the router's own name for this instance
	PoolName      string // the instance's address pool, "" = router-local
	Interface     string // interface note, for comments
}

// SnippetInput is everything the FR-14 config wizard needs to render a NAS's
// copy-paste bootstrap config.
type SnippetInput struct {
	ROSVersion string // "6" or "7"; anything else is treated as 7
	// Services is every enabled instance on the NAS (C8/FR-62): one shared
	// /radius block, a PPPoE AAA block when any pppoe service is enabled, and
	// one /ip hotspot block per hotspot service. Empty falls back to Type.
	Services []ServiceSnippet
	// Type is the legacy single-service kind ("pppoe" | "hotspot"), kept for
	// callers that have not moved to Services. Ignored when Services is set.
	Type         string
	NASName      string   // for comments
	RadiusServer string   // address the router should send RADIUS to (the HikRAD host)
	Secret       string   // shared secret (shown once at creation, FR-13.3)
	SrcAddress   string   // optional router source address for /radius
	CoAPort      int      // /radius incoming port
	InterimSecs  int      // accounting interim-update interval
	WalledGarden []string // hotspot walled-garden hosts (portal/payment/expired-redirect)
}

// HealthFinding is one router-side problem CheckHealth found. Code is a stable
// key the panel localizes; Detail carries the router's own strings (pool names,
// ids) that make the finding actionable. Fix is the exact command that resolves
// it — the operator is being told their router is misconfigured, so the least
// HikRAD can do is say precisely how to fix it rather than leave them guessing.
type HealthFinding struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
	// Fixable reports whether HikRAD can apply Fix itself via ApplyHealthFix.
	// When false the operator must run Fix by hand — so the panel keeps showing
	// the command either way, and only offers the button when this is true.
	Fixable bool `json:"fixable"`
}

// Health finding codes.
const (
	// HealthHotspotUserProfilePool: the default hotspot user profile assigns an
	// address pool, so every RADIUS hotspot login is re-assigned from it at
	// login time — overriding the hotspot server's own pool.
	HealthHotspotUserProfilePool = "hotspot_user_profile_pool"
	// HealthHotspotUserProfilePoolMissing: ...and that pool does not exist, so
	// every RADIUS hotspot login on this router fails ("no address from ip
	// pool") while RADIUS reports a clean accept.
	HealthHotspotUserProfilePoolMissing = "hotspot_user_profile_pool_missing"
)

// Adapter maps abstract intents to concrete vendor behavior.
type Adapter interface {
	// Name is the vendor key stored in nas.vendor.
	Name() string
	// Apply encodes attrs onto an outgoing RADIUS packet (the CoA path). It
	// ignores intents it cannot express in-place so the caller can fall back
	// to Disconnect (FR-15.4).
	Apply(p *radius.Packet, attrs []Attr) error
	// Snippet renders the FR-14 copy-paste NAS config.
	Snippet(in SnippetInput) (string, error)
	// ComposeRate renders a RateSpec (base rate + optional burst/priority) into
	// the concrete vendor rate string used as the rate_limit intent value
	// (FR-11 burst syntax). An empty base rate yields "".
	ComposeRate(spec RateSpec) string
	// ResolveService maps an Access-Request's identifying attributes to one of
	// the NAS's service instances (FR-62 / C7). Vendor-specific attribute
	// parsing — how this vendor encodes a hotspot server name, what its
	// NAS-Port-Type values mean — lives ONLY here (FR-17).
	//
	// candidates is the NAS's ENABLED instances. Returning false means "cannot
	// say which instance this is", which the engine turns into a
	// nas_not_allowed reject rather than guessing — an ambiguous match would
	// otherwise hand the session another zone's address pool.
	ResolveService(q ServiceQuery, candidates []ServiceInstance) (ServiceInstance, bool)
	// DiscoverServices reads the router's existing PPPoE/Hotspot service
	// instances (FR-62.6) so the operator imports them instead of retyping the
	// names C7 must match exactly. Read-only: print sentences only, never
	// add/set. Vendor-specific RouterOS paths/fields live only here (FR-17).
	DiscoverServices(conn ROSConn) ([]DiscoveredService, error)
	// ReadPools lists the router's IP pools (FR-108). Read-only, print only —
	// the adopt flow's source, and how a "named in HikRAD, missing on the
	// router" mismatch becomes visible.
	ReadPools(conn ROSConn) ([]RouterPool, error)
	// PPPoEPoolNames returns the set of IP-pool names the router's PPPoE service
	// actually references (FR-108). On RouterOS a PPPoE client's address comes
	// from the pool named by its PPP profile's remote-/local-address, so these
	// are the only pools that belong to PPPoE — and thus the only ones the adopt
	// flow should import (pools here are PPPoE-only; a DHCP/hotspot pool must not
	// be swept in). Read-only, print only. Vendor RouterOS paths live only here.
	PPPoEPoolNames(conn ROSConn) (map[string]bool, error)
	// PlanPool produces the additive plan that reconciles the router's /ip pool
	// with a HikRAD-OWNED pool (FR-108). Never called for a router-adopted pool
	// — that refusal is enforced by the caller, which owns the ownership column.
	// Plans only; the caller applies via ApplyAutoSetup after a hash match.
	PlanPool(conn ROSConn, in PoolPushInput) (AutoSetupPlan, error)
	// CheckHealth reports router-side configuration that will break HikRAD
	// logins even though HikRAD itself is behaving correctly (FR-62.7). It is
	// the answer to "RADIUS says accept and the user still can't get on" —
	// conditions the operator cannot see from HikRAD and would not think to
	// check on the router. Read-only. An empty result means nothing known-bad
	// was found, never "healthy" in any stronger sense.
	CheckHealth(conn ROSConn) ([]HealthFinding, error)
	// ApplyHealthFix writes the fix for ONE finding code (FR-62.7). Deliberately
	// keyed on a code rather than taking a command: the set of writes it can
	// ever make is closed and lives here, so no caller — and no request body —
	// can turn this into "run arbitrary commands on the router".
	//
	// An unknown or unfixable code is an error, never a silent no-op. Callers
	// re-run CheckHealth first and only pass a code it currently reports, so a
	// stale panel cannot apply a fix to a router that no longer needs it.
	ApplyHealthFix(conn ROSConn, code string) error
	// SupportsInPlace reports whether an in-place CoA-Request change for
	// intent is known to take effect on an already-active session for a NAS
	// of type nasType ("pppoe"|"hotspot") running rosVersion — the Phase 4
	// ROS quirk matrix (docs/ops/ros-matrix.md) encoded as code instead of
	// left to a NAK/timeout round trip (FR-15.4, "version-aware instead of
	// NAK-reactive where knowable"). false means the caller should go
	// straight to Disconnect; the CoA layer consults this before ever
	// sending a packet.
	SupportsInPlace(rosVersion, nasType, intent string) bool
	// PlanAutoSetup computes the FR-56.2 preview: it connects read-only
	// (print/query sentences only, never add/set) and diffs the router's
	// current state against the config the FR-14.2 bootstrap needs, returning
	// additive-only items plus any conflicts that must abort the whole apply.
	// RouterOS API traffic for auto-setup lives only here and in the conn
	// implementation (FR-56.4/FR-17.1).
	//
	// resolutions (v2 phase 3, FR-66.2) maps a PlanConflict.Key to "update" or
	// "keep"; any other value (including absent) means "abort" — an empty or
	// nil map reproduces PRE-FR-66 behavior exactly (C1). "update" only ever
	// turns a Resolvable conflict into a PlanItem that /sets the router's
	// existing entry to HikRAD's value — it never adds or removes anything.
	// "keep" drops the item from both Items and Conflicts: the operator
	// explicitly accepted the router's current state for it.
	PlanAutoSetup(conn ROSConn, in SnippetInput, resolutions map[string]string) (AutoSetupPlan, error)
	// ApplyAutoSetup executes plan's items in order against conn (already
	// re-validated conflict-free by the caller) and reports a per-item
	// result. Callers stop issuing further items after the first failure.
	ApplyAutoSetup(conn ROSConn, plan AutoSetupPlan) []ApplyResult
	// ReadConfig reads the router's current RADIUS-relevant state (v2 phase 3,
	// FR-65). Pure print sentences, same ROSConn seam as PlanAutoSetup/
	// DiscoverServices/CheckHealth — never writes. Vendor-specific paths/
	// fields live only here (FR-17).
	ReadConfig(conn ROSConn) (ConfigSnapshot, error)
	// PlanService computes the FR-67.3/67.4 preview for creating or editing
	// ONE system-managed server instance: read-only connect, additive/update-
	// only writes, same PlanItem/PlanConflict/AutoSetupPlan vocabulary as
	// PlanAutoSetup so the HTTP layer, hashing, and panel rendering are shared
	// code rather than a parallel implementation. Conflicts here are
	// abort-only (Resolvable always false) — there is no safe "update" meaning
	// for "another service already claims this identity."
	PlanService(conn ROSConn, in ServiceProvisionInput) (AutoSetupPlan, error)
	// ApplyService executes a PlanService result exactly like ApplyAutoSetup
	// (same whole-apply-abort-on-first-failure contract).
	ApplyService(conn ROSConn, plan AutoSetupPlan) []ApplyResult

	// --- v4-6 (Router manager I, FR-139-143): read-only device inspection ----
	// Every method below is a pure print/read, same ROSConn contract as
	// ReadConfig/PlanAutoSetup/DiscoverServices — called ONLY by the device
	// worker (radius/device_worker.go), never from an HTTP request handler, so
	// a slow or dead router can never hang a page (frozen contract, phase
	// brief). Vendor-specific RouterOS paths/fields live only here (FR-17).

	// ReadDeviceMonitor is one device-worker tick's sample for the monitor
	// grid (FR-139) and the Overview tab's identity block (FR-141): system
	// resource plus active-session counts, in one read-only round trip.
	ReadDeviceMonitor(conn ROSConn) (DeviceMonitorSample, error)
	// ReadInterfaces lists every interface's live state and byte counters
	// (FR-141 Interfaces tab). The worker diffs consecutive byte counters
	// itself to derive the rx/tx bps written to interface_traffic.
	ReadInterfaces(conn ROSConn) ([]InterfaceState, error)
	// ReadGeneral reads identity, clock/timezone and NTP client config
	// (FR-142 General tab).
	ReadGeneral(conn ROSConn) (GeneralInfo, error)
	// ReadBridges lists bridges and their member ports (FR-143 Bridge tab).
	ReadBridges(conn ROSConn) ([]BridgeInfo, error)
	// ReadVLANs lists VLAN interfaces (FR-143 VLAN tab).
	ReadVLANs(conn ROSConn) ([]VLANInfo, error)
	// ReadIPTab reads static addresses, the DHCP client list, DHCP servers and
	// their live lease table (FR-143 IP tab) in one call.
	ReadIPTab(conn ROSConn) (IPTabSnapshot, error)
	// ReadDNS reads resolver servers, cache stats and static entries (FR-143
	// DNS tab).
	ReadDNS(conn ROSConn) (DNSInfo, error)

	// Reboot issues the router's reboot command (FR-142). The ONLY write this
	// phase performs — everything above is read-only; every caller of Reboot
	// is double-confirmed, permission-gated and audited before this is ever
	// reached (device_api.go).
	Reboot(conn ROSConn) error
	// Ping runs a bounded ping from the router itself (FR-142's diagnostic
	// tool) and summarizes the result. Structurally a read (RouterOS's /ping
	// is a query command, not an add/set), so it does not violate the
	// read-only contract, though — like Reboot — it is still an action rather
	// than a config inspection and is gated the same way.
	Ping(conn ROSConn, target string, count int) (PingResult, error)

	// --- v4-7 (Router manager II, FR-144-149): write support -----------------
	// Every method below that WRITES to the router does so only through a
	// PlanXxx call producing an AutoSetupPlan (the SAME PlanItem/PlanConflict/
	// add-or-set-never-remove vocabulary as PlanAutoSetup/PlanService/
	// PlanPool) executed by the existing ApplyAutoSetup — no new apply
	// semantics anywhere in this phase (the frozen contract). Reads follow the
	// same print-only pattern as every v4-6 ReadXxx method.

	// DetectWizard runs every FR-144 wizard step's read-only probe in one
	// router session and reports, per step, whether the router already has
	// what that step would configure — gate item 2's "already-correct steps
	// are skipped, not re-pushed". Read-only; reuses ReadBridges/ReadDNS/
	// ReadGeneral/ReadConfig/DiscoverServices/ReadWANUplinks internally rather
	// than a second read path for the same objects.
	DetectWizard(conn ROSConn) (WizardDetection, error)
	// PlanWizard computes the FR-144 review step's combined preview across
	// every step the operator filled in: bridge/ports, DNS, NTP, WAN uplinks,
	// and firewall NAT/expired-pool are planned by new logic in this file;
	// RADIUS wiring and per-server creation call PlanAutoSetup/PlanService
	// internally and fold their Items/Conflicts in, so the wizard and the
	// standalone auto-setup/service-create flows can never describe two
	// different "desired state"s for the same router object. A nil field in
	// WizardInput means that step was skipped (already correct, or the
	// operator chose not to touch it) — no items are planned for it.
	PlanWizard(conn ROSConn, in WizardInput, resolutions map[string]string) (AutoSetupPlan, error)

	// ReadWANUplinks lists the router's current WAN-facing interface config
	// (FR-145 Internet tab): static addresses, DHCP clients, and PPPoE
	// clients, tagged by mode. Read-only.
	ReadWANUplinks(conn ROSConn) ([]WANUplinkInfo, error)
	// PlanWANUplink plans ONE WAN uplink (static/DHCP/PPPoE-client) the same
	// additive/update-or-conflict way as every other object here. Used both
	// standalone (Internet tab) and folded into PlanWizard.
	PlanWANUplink(conn ROSConn, in WizardWANInput) (AutoSetupPlan, error)

	// ReadFirewallRules lists the router's current /ip/firewall/filter and
	// /ip/firewall/nat rules (FR-146). Read-only.
	ReadFirewallRules(conn ROSConn) ([]FirewallRule, error)
	// PlanFirewallChange plans a firewall change (FR-146) the same additive/
	// update-or-conflict way as every other object here.
	PlanFirewallChange(conn ROSConn, in FirewallChangeInput) (AutoSetupPlan, error)
	// SnapshotFirewallRules reads the CURRENT values of exactly the rules a
	// pending plan is about to touch (matched by the plan's .id/pending-add
	// identity) and computes the revert sentences that would restore them —
	// the FR-146 auto-revert safety net's evidence, captured BEFORE apply
	// writes anything, never reconstructed after the fact.
	SnapshotFirewallRules(conn ROSConn, plan AutoSetupPlan) (FirewallSnapshot, error)

	// ReadUpdateInfo reads the router's RouterOS update channel, installed
	// version, latest-available version (from the last check), and installed
	// package list (FR-147). Read-only; never triggers a check itself —
	// CheckForUpdates does that as a distinct, explicit action.
	ReadUpdateInfo(conn ROSConn) (RouterUpdateInfo, error)
	// CheckForUpdates asks the router to contact MikroTik's update server and
	// report what it finds (FR-147). A real (if minor) network action on the
	// router's part, so — like Reboot/Ping — it is gated as an action, not
	// folded into the read-only device-worker cache.
	CheckForUpdates(conn ROSConn) (RouterUpdateInfo, error)
	// ApplyRouterUpdate installs the currently-available update on channel
	// (FR-147). The ONE other genuinely destructive write in this phase
	// besides Reboot — the caller double-confirms, audits, and warns the
	// device will reboot and sessions will drop, exactly like Reboot.
	ApplyRouterUpdate(conn ROSConn, channel string) error

	// PushHotspotPage writes a hotspot login-page design to router flash
	// (FR-148) for the named hotspot server instance. files is the page's
	// file set (login.html plus any assets) keyed by filename; mode selects
	// which design HikRAD is pushing ("branded_template" or "custom" — never
	// called for "router_default", which is a metadata-only no-router-write
	// state meaning "leave the router's own page alone"). Router flash is the
	// ONLY place this page ever lives — HikRAD holds no copy the router reads
	// from at login time — and a RouterOS upgrade can wipe it, which is why
	// nas_hotspot_pages (0613) tracks the pushed version/ROS-version
	// separately from this write itself.
	PushHotspotPage(conn ROSConn, rosServerName string, files map[string]string) error

	// PlanIPv6 plans the FR-149 DHCPv6-PD pool + PPP-profile attachment for
	// one service instance (delegated-prefix-length, pool construction). Same
	// additive/update-or-conflict vocabulary as everything else in this file.
	PlanIPv6(conn ROSConn, in WizardIPv6Input) (AutoSetupPlan, error)
}

// DeviceMonitorSample is FR-139/FR-141's one-round-trip device sample.
type DeviceMonitorSample struct {
	Version      string
	BoardName    string
	Identity     string
	SerialNumber string
	CPUPercent   float64
	CPUCores     int
	MemPercent   float64
	SessionCount int
	UptimeSecs   int64
}

// InterfaceState is one Interfaces-tab row (FR-141), byte counters included so
// the worker can derive a bps rate from two consecutive reads.
type InterfaceState struct {
	Name     string
	Type     string
	Running  bool
	Disabled bool
	MAC      string
	MTU      int
	Comment  string
	RxBytes  int64
	TxBytes  int64
}

// GeneralInfo is the FR-142 General tab's identity/time/NTP block.
type GeneralInfo struct {
	Identity   string
	TimeZone   string
	Time       string
	Date       string
	NTPEnabled bool
	NTPMode    string
	NTPServers []string
}

// BridgePort is one member port of a BridgeInfo.
type BridgePort struct {
	Interface string
	PVID      int
}

// BridgeInfo is one /interface/bridge row plus its member ports (FR-143).
type BridgeInfo struct {
	Name    string
	Comment string
	Ports   []BridgePort
}

// VLANInfo is one VLAN interface (FR-143).
type VLANInfo struct {
	Name      string
	VLANID    int
	Interface string
	Comment   string
}

// IPAddressInfo is one static /ip/address row (FR-143).
type IPAddressInfo struct {
	Address   string
	Interface string
	Comment   string
	Disabled  bool
}

// DHCPClientInfo is one /ip/dhcp-client row (FR-143).
type DHCPClientInfo struct {
	Interface string
	Status    string
	Address   string
	Gateway   string
}

// DHCPServerInfo is one /ip/dhcp-server row (FR-143). PoolName is reported so
// the caller can badge it against a HikRAD-owned ip_pools row (FR-108) —
// the only place the IP tab's ownership badge can be anything but "router".
type DHCPServerInfo struct {
	Name      string
	Interface string
	PoolName  string
	Disabled  bool
}

// DHCPLease is one live lease (FR-143's "operator's answer to which device is
// that" — never synthesized, always the router's own /ip/dhcp-server/lease
// table).
type DHCPLease struct {
	Address   string
	MAC       string
	Hostname  string
	Server    string
	Status    string
	ExpiresIn string
}

// IPTabSnapshot is ReadIPTab's whole result (FR-143).
type IPTabSnapshot struct {
	Addresses   []IPAddressInfo
	DHCPClients []DHCPClientInfo
	DHCPServers []DHCPServerInfo
	Leases      []DHCPLease
}

// DNSStaticEntry is one /ip/dns/static row (FR-143).
type DNSStaticEntry struct {
	Name    string
	Address string
	Comment string
}

// DNSInfo is the FR-143 DNS tab's whole result.
type DNSInfo struct {
	Servers             []string
	AllowRemoteRequests bool
	CacheSize           int
	CacheUsed           int
	Static              []DNSStaticEntry
}

// PingResult summarizes FR-142's router-originated ping tool.
type PingResult struct {
	Sent              int
	Received          int
	PacketLossPercent float64
	MinRttMs          float64
	AvgRttMs          float64
	MaxRttMs          float64
}

// ROSConn is a minimal connected RouterOS API session. Every RouterOS API
// client call for auto-setup goes through this seam (FR-56.4) — production
// code dials a real router (see DialROS); tests fake a router's state without
// any network or hardware dependency.
type ROSConn interface {
	// Read runs a read-only (print) sentence and returns each reply row
	// (RouterOS's "!re" sentences) as its word->value map.
	Read(sentence ...string) ([]map[string]string, error)
	// Write runs a mutating (add/set) sentence and returns the router's
	// "!done" reply map (often empty) or an error — e.g. RouterOS's own
	// "failure: already have such entry" for a genuine race.
	Write(sentence ...string) (map[string]string, error)
	Close() error
}

// PlanItem is one additive-only change auto-setup would make (C6 preview
// shape). Command is the exact, human-readable RouterOS command so an
// operator can sanity-check it before approving; Sentence is the actual API
// words used at apply time and is never serialized to the client — the apply
// endpoint always recomputes it server-side rather than trusting anything
// echoed back by a caller (tamper safety, and it's how a stale preview is
// detected: see AutoSetupPlan.Hash).
type PlanItem struct {
	Action       string   `json:"action"` // "add" | "set"
	Path         string   `json:"path"`
	Command      string   `json:"command"`
	CurrentState string   `json:"current_state"`
	Sentence     []string `json:"-"`
}

// PlanConflict is one reason auto-setup refuses to touch the router at all
// (FR-56.2 safety contract: any conflict aborts the whole apply, nothing is
// written) — UNLESS the operator picks an "update" resolution for it (v2
// phase 2, FR-66.2), which is safe only when the conflict came with a
// computable target sentence.
type PlanConflict struct {
	Path     string `json:"path"`
	Existing string `json:"existing"`
	Reason   string `json:"reason"`
	// Key identifies this conflict across the preview/apply round trip so the
	// operator's resolution choice can be supplied and re-verified server-side
	// (FR-66.2/C4). Stable per plan shape: today at most one conflict occurs
	// per Path, so Key == Path.
	Key string `json:"key"`
	// Resolvable is true when an "update" resolution has a computable target
	// sentence (updateSentence below). False for a conflict with no single
	// safe target to rewrite (v1: the hotspot-profile conflict on a router
	// whose zone uses a non-"default" profile name — FR-67's adopt flow is
	// the answer for that router, not a forced guess here).
	Resolvable bool `json:"resolvable"`
	// UpdateCommand is the human-readable command an "update" resolution would
	// run, shown next to the keep/update/abort choice so the operator approves
	// an exact sentence — same transparency PlanItem.Command already gives
	// additive items. Empty when Resolvable is false.
	UpdateCommand string `json:"update_command,omitempty"`
	// updateSentence is the actual API words an "update" resolution would send
	// — never serialized (mirrors PlanItem.Sentence's json:"-" pattern and the
	// same tamper-safety reasoning: apply always recomputes it server-side).
	updateSentence []string `json:"-"`
}

// AutoSetupPlan is PlanAutoSetup's result. Items is empty-safe (nil marshals
// to `[]`); Conflicts non-empty means apply must refuse.
type AutoSetupPlan struct {
	Items     []PlanItem
	Conflicts []PlanConflict
}

// ApplyResult is one executed item's outcome (C6 "per-item results").
type ApplyResult struct {
	Path    string `json:"path"`
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// ConfigSnapshot is FR-65's read-only view of a router's current
// RADIUS-relevant state (v2 phase 3, C2). Every field comes from a print
// sentence; nothing here is ever written.
type ConfigSnapshot struct {
	RadiusEntries   []RadiusEntryConfig
	RadiusIncoming  RadiusIncomingConfig
	PPPAAA          PPPAAAConfig
	HotspotProfiles []HotspotProfileConfig
	WalledGarden    []string
}

// RadiusEntryConfig is one /radius row. SecretPresent reports whether the
// router returned a secret value at all (RouterOS API permissions can
// withhold it) — the secret itself is never surfaced (FR-65.1).
type RadiusEntryConfig struct {
	Address       string
	Service       string
	Comment       string
	SrcAddress    string
	SecretPresent bool
}

// RadiusIncomingConfig is the router-wide CoA listener toggle.
type RadiusIncomingConfig struct {
	Accept bool
	Port   int
}

// PPPAAAConfig is the /ppp/aaa RADIUS toggle for PPPoE.
type PPPAAAConfig struct {
	UseRadius         bool
	Accounting        bool
	InterimUpdateSecs int
}

// HotspotProfileConfig is one /ip/hotspot/profile row's RADIUS-relevant
// fields.
type HotspotProfileConfig struct {
	Name              string
	UseRadius         bool
	InterimUpdateSecs int
}

// ServiceProvisionInput is FR-67.3/67.4's create-or-edit-one-server input.
// Deliberately separate from SnippetInput/AutoSetupPlan's whole-NAS RADIUS
// wiring vocabulary — this describes ONE server object (interface binding,
// local address, pool) plus the shared RADIUS values that server needs wired
// to HikRAD.
type ServiceProvisionInput struct {
	Kind          string // "pppoe" | "hotspot"
	ROSServerName string // required; must be unique among the NAS's other enabled instances of Kind
	Label         string
	Interface     string // required: the router interface/bridge this server binds
	LocalAddress  string // hotspot only: the server's own gateway IP
	AddressRange  string // hotspot only, optional: DHCP range for a HikRAD-created pool
	PoolName      string // resolved HikRAD ip_pool name, or "" = router-local
	Values        SnippetInput
	// Editing is true for FR-67.4 (edit a system-managed instance): the
	// planner requires a matching router-side object (by ROSServerName) to
	// already exist and only ever /sets it, rather than /add-ing a new one.
	// False (FR-67.3 create) requires the OPPOSITE — no existing match — so
	// create can never silently become an edit of some unrelated object that
	// happens to share a name, or vice versa.
	Editing bool
}

// --- v4-7 (Router manager II, FR-144-149) types -----------------------------

// WizardStepStatus is DetectWizard's per-step verdict (gate item 2).
type WizardStepStatus struct {
	AlreadyCorrect bool   `json:"already_correct"`
	Summary        string `json:"summary"`
}

// WizardDetection is DetectWizard's whole-router probe result, one entry per
// step the wizard can skip. Steps with no meaningful "already correct" state
// (e.g. the review step itself) are simply absent.
type WizardDetection struct {
	Bridge   WizardStepStatus `json:"bridge"`
	DNS      WizardStepStatus `json:"dns"`
	NTP      WizardStepStatus `json:"ntp"`
	WAN      WizardStepStatus `json:"wan"`
	RADIUS   WizardStepStatus `json:"radius"`
	Services WizardStepStatus `json:"services"`
}

// WizardBridgeInput is the FR-144 bridge + bridge-ports step's form.
type WizardBridgeInput struct {
	Name  string   `json:"name"`
	Ports []string `json:"ports"`
}

// WizardDNSInput is the FR-144 DNS step's form (Google/Cloudflare/Quad9
// presets resolve to concrete server IPs client-side before this reaches the
// backend — the plan only ever sees the resolved addresses it must set).
type WizardDNSInput struct {
	Servers             []string `json:"servers"`
	AllowRemoteRequests bool     `json:"allow_remote_requests"`
	CacheSize           int      `json:"cache_size"`
}

// WizardNTPInput is the FR-144 NTP step's form.
type WizardNTPInput struct {
	Servers []string `json:"servers"`
}

// WizardWANInput is one uplink from the FR-144 Internet step / the FR-145
// Internet tab's create form.
type WizardWANInput struct {
	Interface     string `json:"interface"`
	Label         string `json:"label"`
	Mode          string `json:"mode"` // "static" | "dhcp" | "pppoe"
	Address       string `json:"address"`
	Gateway       string `json:"gateway"`
	PPPoEUser     string `json:"pppoe_user"`
	PPPoEPassword string `json:"pppoe_password"`
}

// WizardExpiredPoolInput is the details/firewall step's expired-subscriber
// walled-garden pool.
type WizardExpiredPoolInput struct {
	PoolName    string   `json:"pool_name"`
	Ranges      []string `json:"ranges"`
	RedirectURL string   `json:"redirect_url"`
}

// WizardFirewallInput is the FR-144 details/firewall step's NAT + expired-
// pool form (rule-editing on the dedicated FR-146 Firewall tab is a separate,
// later action — this step only covers what a working NAS needs day one:
// internet-sharing NAT for the WAN uplinks and, optionally, the expired
// redirect pool).
type WizardFirewallInput struct {
	NATOutInterfaces []string                 `json:"nat_out_interfaces"`
	ExpiredPool      *WizardExpiredPoolInput  `json:"expired_pool"`
}

// WizardIPv6Input is the FR-149 opt-in IPv6 step, applied per service
// instance that opts in.
type WizardIPv6Input struct {
	Enabled       bool   `json:"enabled"`
	ROSServerName string `json:"ros_server_name"` // which service instance this pool attaches to
	Prefix        string `json:"prefix"`           // the ISP-delegated base prefix, e.g. "2001:db8::/32"
	PrefixLen     int    `json:"prefix_len"`       // per-subscriber delegated length, 48-64, default 56
	PoolName      string `json:"pool_name"`
}

// WizardInput is PlanWizard's whole-review-step input (FR-144). A nil
// pointer field means that step was skipped — no items are planned for it.
// RADIUS/Services reuse the exact types PlanAutoSetup/PlanService already
// take, so the wizard's "services" step and the standalone FR-67 service
// create/edit form describe one desired state, not two.
type WizardInput struct {
	Bridge       *WizardBridgeInput
	DNS          *WizardDNSInput
	NTP          *WizardNTPInput
	WAN          []WizardWANInput
	RadiusValues SnippetInput
	Services     []ServiceProvisionInput
	Firewall     *WizardFirewallInput
	IPv6         []WizardIPv6Input
}

// WANUplinkInfo is one FR-145 Internet-tab row as read from the router.
type WANUplinkInfo struct {
	Interface string `json:"interface"`
	Mode      string `json:"mode"` // "static" | "dhcp" | "pppoe"
	Address   string `json:"address"`
	Gateway   string `json:"gateway"`
	Status    string `json:"status"`
	Comment   string `json:"comment"`
}

// FirewallRule is one /ip/firewall/filter or /ip/firewall/nat row (FR-146).
type FirewallRule struct {
	ID          string `json:"id"`
	Chain       string `json:"chain"`
	Table       string `json:"table"` // "filter" | "nat"
	Action      string `json:"action"`
	SrcAddress  string `json:"src_address,omitempty"`
	DstAddress  string `json:"dst_address,omitempty"`
	OutInterface string `json:"out_interface,omitempty"`
	InInterface  string `json:"in_interface,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	DstPort     string `json:"dst_port,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Disabled    bool   `json:"disabled"`
	Ownership   string `json:"ownership"` // "system" | "router", computed by the caller from hikradComment
}

// FirewallChangeInput is one FR-146 firewall-tab edit: either a new rule
// (RuleID empty) or a field change to an existing HikRAD-tagged rule.
type FirewallChangeInput struct {
	RuleID string            `json:"rule_id"`
	Table  string            `json:"table"` // "filter" | "nat"
	Fields map[string]string `json:"fields"`
}

// FirewallSnapshot is SnapshotFirewallRules' result: the pre-change state of
// every rule PlanFirewallChange's plan is about to touch, plus the exact
// sentences that would restore it. Both are stored verbatim in
// nas_firewall_snapshots (0612) so the auto-revert worker never has to
// recompute "the opposite of what we just did" from a router that may, by
// the time revert runs, be the very thing that's unreachable.
type FirewallSnapshot struct {
	Rules           []FirewallRule `json:"rules"`
	RevertSentences [][]string     `json:"revert_sentences"`
}

// RouterUpdateInfo is the FR-147 Update tab's whole read (channel, current
// vs. latest-known version, and the installed package list).
type RouterUpdateInfo struct {
	Channel         string          `json:"channel"`
	InstalledVersion string         `json:"installed_version"`
	LatestVersion   string          `json:"latest_version"`
	UpdateAvailable bool            `json:"update_available"`
	Packages        []RouterPackage `json:"packages"`
}

// RouterPackage is one /system/package/print row.
type RouterPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

var registry = map[string]Adapter{}

// Register installs an adapter under its vendor key. Called from adapter
// init(); the parent package blank-imports this package so registration runs.
func Register(a Adapter) { registry[a.Name()] = a }

// For returns the adapter for a vendor key, falling back to MikroTik (the only
// certified v1 vendor) for an empty or unknown key so a mis-set nas.vendor
// still authenticates rather than failing closed.
func For(name string) Adapter {
	if a, ok := registry[name]; ok {
		return a
	}
	return registry["mikrotik"]
}
