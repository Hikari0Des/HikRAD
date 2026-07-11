package radius

// Hotspot voucher login (FR-18.1, contract: voucher becomes the credential).
// A MikroTik Hotspot posts the voucher code as the RADIUS username; the
// authorize path detects a plausible voucher, then hands it to D's redemption
// logic (05/FR-22) through the injected VoucherAuthenticator seam — B never
// imports billing. Redemption owns the single-use/charging semantics (C3); B
// only decides accept/reject from the AuthView it returns.
//
// Voucher-format detection guards against pounding the redeemer with ordinary
// usernames and honours D's C3 batch-prefix discipline when one is configured:
// a code must be plausibly voucher-shaped (and prefix-matching, if set) before a
// redeem is attempted, so a real username that happens to be unknown is not
// mistaken for a voucher.

import (
	"context"
	"strings"
	"sync"
)

// VoucherAuthenticator is D's hotspot-voucher redemption seam. AuthenticateVoucher
// validates+redeems code and returns the AuthView to authorize the session; ok
// is false for an invalid/spent/void voucher (→ reject). A non-nil error is an
// infrastructure failure (→ 500, FreeRADIUS rejects rather than hangs).
type VoucherAuthenticator interface {
	AuthenticateVoucher(ctx context.Context, code string) (AuthView, bool, error)
}

var (
	voucherMu     sync.RWMutex
	voucherAuth   VoucherAuthenticator
	voucherPrefix string // optional required prefix (D's C3 batch prefix discipline)
)

// SetVoucherAuthenticator installs D's redemption seam (called once at boot).
func SetVoucherAuthenticator(a VoucherAuthenticator) {
	voucherMu.Lock()
	voucherAuth = a
	voucherMu.Unlock()
}

// SetVoucherPrefix constrains voucher detection to codes with this prefix
// (empty = shape-only detection). Wired from settings by D/A when batches use a
// house prefix; prevents voucher/username collisions (edge case).
func SetVoucherPrefix(prefix string) {
	voucherMu.Lock()
	voucherPrefix = prefix
	voucherMu.Unlock()
}

func currentVoucherAuth() (VoucherAuthenticator, string) {
	voucherMu.RLock()
	defer voucherMu.RUnlock()
	return voucherAuth, voucherPrefix
}

// plausibleVoucher reports whether code is voucher-shaped enough to attempt a
// redemption: non-empty, no whitespace, bounded length, alphanumeric with '-'
// or '_' separators, and matching the configured prefix when one is set. Pure
// and case-insensitive on the prefix.
func plausibleVoucher(code, prefix string) bool {
	code = strings.TrimSpace(code)
	if len(code) < 4 || len(code) > 64 {
		return false
	}
	if prefix != "" && !strings.HasPrefix(strings.ToUpper(code), strings.ToUpper(prefix)) {
		return false
	}
	for _, r := range code {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// tryVoucher attempts a hotspot voucher login for code. found is true only when
// the voucher redeemed successfully; err signals infrastructure failure.
func (e *engine) tryVoucher(ctx context.Context, code string) (AuthView, bool, error) {
	auth, prefix := currentVoucherAuth()
	if auth == nil {
		return AuthView{}, false, nil // D not wired: no voucher logins
	}
	if !plausibleVoucher(code, prefix) {
		return AuthView{}, false, nil
	}
	view, ok, err := auth.AuthenticateVoucher(ctx, code)
	if err != nil {
		return AuthView{}, false, err
	}
	return view, ok, nil
}
