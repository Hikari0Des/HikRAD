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
	Service         string // coarse hint: "pppoe" | "hotspot"
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
	// current state against the config in against desired the FR-14.2
	// bootstrap needs, returning additive-only items plus any conflicts that
	// must abort the whole apply. RouterOS API traffic for auto-setup lives
	// only here and in the conn implementation (FR-56.4/FR-17.1).
	PlanAutoSetup(conn ROSConn, in SnippetInput) (AutoSetupPlan, error)
	// ApplyAutoSetup executes plan's items in order against conn (already
	// re-validated conflict-free by the caller) and reports a per-item
	// result. Callers stop issuing further items after the first failure.
	ApplyAutoSetup(conn ROSConn, plan AutoSetupPlan) []ApplyResult
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
// written).
type PlanConflict struct {
	Path     string `json:"path"`
	Existing string `json:"existing"`
	Reason   string `json:"reason"`
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
