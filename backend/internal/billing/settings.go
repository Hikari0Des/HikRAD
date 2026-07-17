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
	permPaymentTicketsVerify = "payment_tickets.verify" // v2-2, FR-79.2 (generalizes Phase 4's card_payments.verify)
	permProvidersManage      = "payment_providers.manage" // v2-2, FR-77.1
)

// Settings keys.
const (
	keyRenewalAnchor  = "billing.renewal_anchor"       // "from_expiry" (default) | "from_now"
	keyAdminBypass    = "billing.admin_balance_bypass" // bool, default true
	keyReceiptPrefix  = "billing.receipt_prefix"       // string, default "HR-"
	keyReceiptBrand   = "billing.receipt_branding"     // object, see brandingConfig
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

// brandingConfig is the receipt header (FR-21). All fields optional.
type brandingConfig struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

func (m *Module) branding(ctx context.Context) brandingConfig {
	var b brandingConfig
	if m.settings == nil {
		return b
	}
	if v, err := platform.Get[brandingConfig](ctx, m.settings, keyReceiptBrand); err == nil {
		b = v
	}
	return b
}
