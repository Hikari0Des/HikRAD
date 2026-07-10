package radius

// policy.go is the real authorize policy engine (key flow 1 of the master PRD),
// replacing the Phase-1 stub. It resolves D's cached AuthView and runs the
// ordered check chain (FR-5/FR-9/FR-10/FR-58): known NAS → service → credentials
// → status → expiry → quota → session limit → MAC lock, emitting vendor-neutral
// intents (never a VSA — FR-17). Every attempt is recorded to radius:decisions.

import (
	"context"
	"crypto/subtle"
	"strings"
)

// outcomeMode selects the reply-attribute set once the gating checks pass.
type outcomeMode int

const (
	modeNormal outcomeMode = iota
	modeThrottled
	modeExpiredPool
)

// expiredPoolFallbackRate is the "minimal rate" an expired subscriber gets in
// the walled garden when the profile defines no throttle rate — deliberately
// low so a renewal (which lifts them out via CoA) is the obvious next step.
const expiredPoolFallbackRate = "1M/1M"

// decide runs the policy engine. A non-nil error means infrastructure failure
// (DB/Redis unreachable, corrupt ciphertext) — the handler turns it into a 500
// so FreeRADIUS's exec timeout rejects rather than hangs; a legitimate
// accept/reject is always a value, never an error.
func (e *engine) decide(ctx context.Context, req authorizeRequest) (authorizeResponse, error) {
	ev := decisionEvent{Username: req.Username, Service: req.Service, NASIP: req.NasIP}

	// 1. Known NAS (FR-13.2). FreeRADIUS already drops packets from clients with
	// no matching secret; this rejects a registered-secret-but-unregistered-IP
	// case and surfaces it to the debug tool.
	known, err := e.nasKnown(ctx, req.NasIP)
	if err != nil {
		return authorizeResponse{}, err
	}
	if !known {
		return e.reject(ctx, ev, ReasonUnknownNAS), nil
	}
	e.markSeen(ctx, req.NasIP)
	ev.Checks = append(ev.Checks, "nas")

	// 2. Resolve the read-model (needed by the service check below).
	view, found, err := e.resolveView(ctx, req.Username)
	if err != nil {
		return authorizeResponse{}, err
	}
	if !found {
		return e.reject(ctx, ev, ReasonUnknownUser), nil
	}
	ev.Checks = append(ev.Checks, "user")

	// 3. Service check (FR-58): a Hotspot login for a PPPoE subscriber is
	// allowed only when the subscriber opts into Hotspot.
	hotspot := req.Service == "hotspot"
	if hotspot && !view.AllowHotspot {
		return e.reject(ctx, ev, ReasonServiceNotAllowed), nil
	}
	ev.Checks = append(ev.Checks, "service")

	// 4. Credentials — decryption happens ONLY here (NFR-4.2).
	plaintext, err := e.decrypt(view.PasswordEnc)
	if err != nil {
		return authorizeResponse{}, err
	}
	if !credentialsOK(string(plaintext), req) {
		return e.reject(ctx, ev, ReasonBadPassword), nil
	}
	ev.Checks = append(ev.Checks, "credentials")

	// 5. Status disabled.
	if view.Status == "disabled" {
		return e.reject(ctx, ev, ReasonDisabled), nil
	}

	// 6. Expiry (FR-9). block → reject; expired_pool → walled-garden accept.
	mode := modeNormal
	if e.isExpired(view) {
		if view.ExpiryBehavior != "expired_pool" {
			return e.reject(ctx, ev, ReasonExpired), nil
		}
		mode = modeExpiredPool
		ev.Checks = append(ev.Checks, "expired_pool")
	} else if !hotspot && view.QuotaExhausted {
		// 7. Quota (FR-10). Skipped entirely for Hotspot service (FR-58.3).
		switch view.QuotaBehavior {
		case "block":
			return e.reject(ctx, ev, ReasonQuotaExhausted), nil
		case "throttle":
			mode = modeThrottled
			ev.Checks = append(ev.Checks, "throttled")
		case "expired_pool":
			mode = modeExpiredPool
			ev.Checks = append(ev.Checks, "quota_expired_pool")
		}
	}

	// 8. Simultaneous-session limit via C's live counter (FR-5/FR-58.2).
	if rej, ok := e.sessionLimitReject(view, hotspot); ok {
		return e.reject(ctx, ev, rej), nil
	}
	ev.Checks = append(ev.Checks, "session_limit")

	// 9. MAC lock (FR-5) — PPPoE only; Hotspot devices are inherently different
	// MACs so the lock does not apply (task file).
	if !hotspot {
		if rej, ok := e.macLock(ctx, view, req.CallingStationID); ok {
			return e.reject(ctx, ev, rej), nil
		}
		ev.Checks = append(ev.Checks, "mac_lock")
	}

	// Accept: build the vendor-neutral reply intents for the chosen mode.
	resp := authorizeResponse{Action: "accept", Reason: ReasonOK, Attributes: e.replyIntents(view, hotspot, mode)}
	ev.Outcome = "accept"
	ev.Reason = ReasonOK
	e.record(ctx, ev)
	return resp, nil
}

func (e *engine) isExpired(view AuthView) bool {
	if view.Status == "expired" {
		return true
	}
	return !view.ExpiresAt.IsZero() && e.now().After(view.ExpiresAt)
}

// sessionLimitReject applies the simultaneous-session limit. PPPoE counts
// against SessionLimit; Hotspot allows exactly one concurrent Hotspot session
// outside that limit (FR-58.2). The just-authorizing session is not yet in the
// live hash, so "already at the limit" means one more would exceed it.
func (e *engine) sessionLimitReject(view AuthView, hotspot bool) (string, bool) {
	count := currentLiveCount()
	if hotspot {
		if count(view.SubscriberID, "hotspot") >= 1 {
			return ReasonSessionLimit, true
		}
		return "", false
	}
	if view.SessionLimit > 0 && count(view.SubscriberID, "pppoe") >= view.SessionLimit {
		return ReasonSessionLimit, true
	}
	return "", false
}

// macLock enforces the MAC lock for a PPPoE request. learn mode records the
// first MAC and accepts; a later mismatch (learn or fixed) rejects.
func (e *engine) macLock(ctx context.Context, view AuthView, callingStation string) (string, bool) {
	if view.MacLockMode == "" || view.MacLockMode == "off" {
		return "", false
	}
	mac := normalizeMAC(callingStation)
	locked := normalizeMAC(view.LearnedMac)

	if locked == "" {
		// Nothing to compare against yet.
		if view.MacLockMode == "learn" && mac != "" {
			if p := currentProvider(); p != nil {
				if err := p.LearnMac(ctx, view.SubscriberID, mac); err != nil {
					// Best-effort: a learn failure must not deny a legitimate
					// first login; the next login re-attempts the learn.
					e.log.Warn("radius: learn mac failed", "error", err, "subscriber", view.SubscriberID)
				}
			}
		}
		return "", false
	}
	if mac != "" && mac != locked {
		return ReasonMACMismatch, true
	}
	return "", false
}

// replyIntents produces the abstract reply set for the outcome mode. Precedence
// (edge case): a static IP wins over any pool — emit static_ip and omit the
// pool so the vendor adapter maps it to Framed-IP-Address alone.
func (e *engine) replyIntents(view AuthView, hotspot bool, mode outcomeMode) []attribute {
	attrs := make([]attribute, 0, 3)
	add := func(intent Intent, value string) {
		if value != "" {
			attrs = append(attrs, attribute{Intent: string(intent), Value: value})
		}
	}

	switch mode {
	case modeExpiredPool:
		add(IntentAddressPool, view.ExpiredPoolName)
		add(IntentRedirectExpired, "expired")
		rate := view.ThrottleRate
		if rate == "" {
			rate = expiredPoolFallbackRate
		}
		add(IntentRateLimit, rate)
		return attrs
	case modeThrottled:
		rate := view.ThrottleRate
		if rate == "" {
			rate = view.RateLimit
		}
		add(IntentRateLimit, rate)
	default: // modeNormal
		rate := view.RateLimit
		if hotspot && view.HotspotRateLimit != "" {
			rate = view.HotspotRateLimit
		}
		add(IntentRateLimit, rate)
	}

	// Address assignment (normal/throttled). Static IP takes precedence.
	if view.StaticIP != "" {
		add(IntentStaticIP, view.StaticIP)
	} else {
		add(IntentAddressPool, view.PoolName)
	}
	return attrs
}

// reject records the rejected attempt and returns the C4 reject response.
func (e *engine) reject(ctx context.Context, ev decisionEvent, reason string) authorizeResponse {
	ev.Outcome = "reject"
	ev.Reason = reason
	e.record(ctx, ev)
	return authorizeResponse{Action: "reject", Reason: reason, Attributes: []attribute{}}
}

// credentialsOK verifies PAP or CHAP against the cleartext password.
func credentialsOK(password string, req authorizeRequest) bool {
	if req.ChapResponse != "" {
		ok, err := verifyCHAP(password, req.ChapChallenge, req.ChapResponse)
		if err != nil {
			return false
		}
		return ok
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(req.Password)) == 1
}

// normalizeMAC upper-cases a MAC and strips the common separators so
// "aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF" and "AABBCCDDEEFF" compare equal.
// A MikroTik Calling-Station-Id may also append a suffix after a space (e.g.
// "AA:BB:.. host"); keep only the first token.
func normalizeMAC(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	r := strings.NewReplacer(":", "", "-", "", ".", "")
	return strings.ToUpper(r.Replace(s))
}
