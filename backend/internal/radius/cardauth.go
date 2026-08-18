package radius

// Hotspot card login (v4-5, FR-136). A card is a credential exactly like a
// voucher (radius/voucher.go): the code is posted as the username, this
// package resolves it to a full AuthView, and decide()'s EXISTING
// evaluateLimits/replyIntents machinery does everything from there — no
// second enforcement implementation exists for cards (phase brief binding
// rule, gate item 10). The cards package builds that AuthView (mapping a
// card group's own rate/transfer/uptime/expiry-after-first-login/valid-until
// settings onto the same fields a subscriber's profile would populate) and
// registers itself here at boot, mirroring SetVoucherAuthenticator exactly.

import (
	"context"
	"sync"
)

// CardAuthenticator is the cards package's seam. AuthenticateCard validates a
// code against cards+card_groups and returns the AuthView to authorize the
// session; ok is false for an unknown/disabled/past-shelf-date card (→
// reject, falls through to unknown_user exactly like an unrecognized
// voucher). A non-nil error is an infrastructure failure (→ 500).
type CardAuthenticator interface {
	AuthenticateCard(ctx context.Context, code string) (AuthView, bool, error)
}

var (
	cardAuthMu sync.RWMutex
	cardAuth   CardAuthenticator
)

// SetCardAuthenticator installs the cards package's redemption seam (called
// once at boot, same pattern as SetVoucherAuthenticator).
func SetCardAuthenticator(a CardAuthenticator) {
	cardAuthMu.Lock()
	cardAuth = a
	cardAuthMu.Unlock()
}

func currentCardAuth() CardAuthenticator {
	cardAuthMu.RLock()
	defer cardAuthMu.RUnlock()
	return cardAuth
}

// tryCard attempts a hotspot card login for code, using the identical
// shape-check plausibleVoucher already applies to vouchers (bounded-length
// alnum/separator string) — cards have no house-prefix concept to further
// constrain detection with, unlike vouchers' optional SetVoucherPrefix.
func (e *engine) tryCard(ctx context.Context, code string) (AuthView, bool, error) {
	auth := currentCardAuth()
	if auth == nil {
		return AuthView{}, false, nil // cards package not wired: no card logins
	}
	if !plausibleVoucher(code, "") {
		return AuthView{}, false, nil
	}
	view, ok, err := auth.AuthenticateCard(ctx, code)
	if err != nil {
		return AuthView{}, false, err
	}
	return view, ok, nil
}
