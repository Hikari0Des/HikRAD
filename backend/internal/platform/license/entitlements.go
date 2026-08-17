package license

// Entitlement names (FR-122, Decision 62). Checked by string, never by tier
// name — the same "check by permission string, never by role name"
// discipline the panel's auth model already uses. Adding a new tier must
// never require touching an `if tier == "..."` anywhere enforcement lives;
// only this catalogue and the vendor's issuing catalogue change.
const (
	EntitlementWhiteLabel  = "white_label"
	EntitlementSelfService = "self_service"
	EntitlementAPIAccess   = "api_access"
	EntitlementGateways    = "gateways"
)

// KnownEntitlements is every entitlement name this build understands, used
// to build the resolved set shown on the License page and offered by
// scripts/license-tool's issuing prompt. Verify never validates entitlement
// names against this list — an unrecognized string in a v2 payload's
// Entitlements is not an error, it just doesn't match anything here, so a
// newer vendor tool can grant a name an older server binary doesn't
// recognize yet without breaking signature verification.
var KnownEntitlements = []string{
	EntitlementWhiteLabel,
	EntitlementSelfService,
	EntitlementAPIAccess,
	EntitlementGateways,
}

// DefaultEntitlements maps a v1 payload's tier to the entitlement set a v2
// evaluation grants it (FR-122: "v1 blobs must still verify... mapped to a
// default entitlement set derived from their tier. No customer is
// stranded"). white_label is deliberately withheld for every v1 tier: it is
// a brand-new capability (Decision 62) nobody purchased retroactively, so
// not granting it is not "stranding" a customer — it is correctly not
// granting a feature that didn't exist when they bought. The other three
// entitlements gate nothing yet in this phase (reserved names for future
// phases — e.g. gateways for the payment-gateway work Decision 67 defers),
// so granting them to every v1 tier is a safe no-op that avoids an
// upgrade-time regression the day something first checks for them.
func DefaultEntitlements(tier string) []string {
	return []string{EntitlementSelfService, EntitlementAPIAccess, EntitlementGateways}
}
