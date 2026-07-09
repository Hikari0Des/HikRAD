package radius

// Intent is the abstract reply vocabulary contract C4 speaks. FreeRADIUS
// (deploy/freeradius/) maps each intent to a vendor VSA — Mikrotik-Rate-Limit,
// Framed-Pool, Session-Timeout — entirely on its side; this package never
// names a vendor attribute (FR-17 vendor neutrality).
type Intent string

const (
	IntentRateLimit      Intent = "rate_limit"
	IntentAddressPool    Intent = "address_pool"
	IntentSessionTimeout Intent = "session_timeout"
)

// Reject reasons, per the C4 "reason" enum.
const (
	ReasonOK           = "ok"
	ReasonBadPassword  = "bad_password"
	ReasonExpired      = "expired"
	ReasonDisabled     = "disabled"
	ReasonSessionLimit = "session_limit"
	ReasonMACMismatch  = "mac_mismatch"
	ReasonUnknownUser  = "unknown_user"
	ReasonUnknownNAS   = "unknown_nas"
)
