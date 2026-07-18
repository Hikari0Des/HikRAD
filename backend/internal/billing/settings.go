package billing

// Billing configuration read from the platform settings service (FR-53.2). All
// keys have sane offline defaults so a fresh install renews without any prior
// configuration. Anchor rule and receipt branding are user-tunable.

import (
	"context"

	"github.com/hikrad/hikrad/internal/platform"
)

// Permission strings this module introduces (checked by string, never by role
// name — frozen contract C2/C7). renew/topup/export live in the auth package.
const (
	permRefund               = "refund"
	permVouchersView         = "vouchers.view"
	permVouchersCreate       = "vouchers.create"
	permPaymentTicketsVerify = "payment_tickets.verify"   // v2-2, FR-79.2 (generalizes Phase 4's card_payments.verify)
	permProvidersManage      = "payment_providers.manage" // v2-2, FR-77.1
)

// Settings keys.
const (
	keyRenewalAnchor  = "billing.renewal_anchor"       // "from_expiry" (default) | "from_now"
	keyAdminBypass    = "billing.admin_balance_bypass" // bool, default true
	keyReceiptPrefix  = "billing.receipt_prefix"       // string, default "HR-"
	keyReceiptBrand   = "billing.receipt_branding"     // bool, default true — show the instance identity on receipts (v2 phase 11, FR-91/FR-93; was misread as an object pre-phase, see docs/ops/known-issues.md)
	keyVoucherPrefix  = "billing.voucher_prefix"       // string, default ""
	keyCurrency       = "locale.currency"              // string, default "IQD"
	keyReceiptNumeral = "billing.receipt_numerals"     // "auto" (default) | "latin" | "arabic"
)

const (
	anchorFromExpiry = "from_expiry"
	anchorFromNow    = "from_now"
)

// getString returns a string setting or def when unset/undecodable.
func (m *Module) getString(ctx context.Context, key, def string) string {
	if m.settings == nil {
		return def
	}
	v, err := platform.Get[string](ctx, m.settings, key)
	if err != nil {
		return def
	}
	return v
}

// getBool returns a bool setting or def when unset/undecodable.
func (m *Module) getBool(ctx context.Context, key string, def bool) bool {
	if m.settings == nil {
		return def
	}
	v, err := platform.Get[bool](ctx, m.settings, key)
	if err != nil {
		return def
	}
	return v
}

// anchor returns the configured renewal anchor rule (default from_expiry).
func (m *Module) anchor(ctx context.Context) string {
	a := m.getString(ctx, keyRenewalAnchor, anchorFromExpiry)
	if a != anchorFromNow {
		return anchorFromExpiry
	}
	return a
}

// showReceiptBranding reports whether the receipt header (FR-21) should show
// the instance identity (v2 phase 11, FR-91) — default true. Pre-phase this
// key was misread as a {name,address,phone} object (a shape no UI ever
// wrote), which silently failed to unmarshal every time and left receipts
// always showing the hardcoded fallback; see docs/ops/known-issues.md. The
// name/logo themselves come from platform.LoadIdentity, the single corrected
// branding source — this key controls only whether receipt.go applies it.
func (m *Module) showReceiptBranding(ctx context.Context) bool {
	return m.getBool(ctx, keyReceiptBrand, true)
}
