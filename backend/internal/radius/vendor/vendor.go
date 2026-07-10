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

// SnippetInput is everything the FR-14 config wizard needs to render a NAS's
// copy-paste bootstrap config.
type SnippetInput struct {
	ROSVersion   string   // "6" or "7"; anything else is treated as 7
	Type         string   // "pppoe" | "hotspot"
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
