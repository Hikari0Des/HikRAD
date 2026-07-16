package radius

import "github.com/hikrad/hikrad/internal/radius/vendor"

// Intent is the abstract reply vocabulary contract C4 speaks. The vendor
// adapter (internal/radius/vendor) maps each intent to a concrete vendor VSA
// (the rate-limit, framed-pool, framed-IP, session-timeout and address-list
// attributes) — so this package never names a vendor attribute (FR-17 vendor
// neutrality). For the authorize reply the mapping happens in FreeRADIUS
// (scripts/authorize.pl); for CoA it happens in the Go adapter.
type Intent string

const (
	IntentRateLimit       Intent = vendor.IntentRateLimit
	IntentAddressPool     Intent = vendor.IntentAddressPool
	IntentSessionTimeout  Intent = vendor.IntentSessionTimeout
	IntentRedirectExpired Intent = vendor.IntentRedirectExpired
	IntentStaticIP        Intent = vendor.IntentStaticIP
)

// Reject reasons, per the C4 "reason" enum (+ extensions this phase adds:
// service_not_allowed for FR-58 and quota_exhausted for the FR-10 block path).
const (
	ReasonOK                = "ok"
	ReasonBadPassword       = "bad_password"
	ReasonExpired           = "expired"
	ReasonDisabled          = "disabled"
	ReasonSessionLimit      = "session_limit"
	ReasonMACMismatch       = "mac_mismatch"
	ReasonUnknownUser       = "unknown_user"
	ReasonUnknownNAS        = "unknown_nas"
	ReasonServiceNotAllowed = "service_not_allowed"
	ReasonQuotaExhausted    = "quota_exhausted"
	// ReasonNASNotAllowed (FR-64) means the NAS/service instance forbids this
	// session — either the subscriber is scoped elsewhere, or the request could
	// not be resolved to an enabled service instance on this NAS (C6 step 2).
	// Deliberately distinct from service_not_allowed, which means the SUBSCRIBER
	// may not use the service: one is an operator config fact, the other an
	// account fact, and FR-39's debug view must tell them apart.
	ReasonNASNotAllowed = "nas_not_allowed"
)
