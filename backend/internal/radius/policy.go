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

	"github.com/hikrad/hikrad/internal/radius/vendor"
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
	nas, known, err := e.nasByIP(ctx, req.NasIP)
	if err != nil {
		return authorizeResponse{}, err
	}
	if !known {
		return e.reject(ctx, ev, ReasonUnknownNAS), nil
	}
	e.markSeen(ctx, req.NasIP)
	ev.Checks = append(ev.Checks, "nas")

	// 2. Resolve the service instance (FR-62 / C6 step 2). Which of the NAS's
	// services this request belongs to decides the reply's address pool and
	// whether an FR-64 service scope is satisfied, so it is settled before any
	// subscriber lookup. A failure here is a NAS-configuration fact, not an
	// account one — hence nas_not_allowed, not service_not_allowed.
	instance, ok, err := e.resolveInstance(ctx, nas, req)
	if err != nil {
		return authorizeResponse{}, err
	}
	if !ok {
		return e.reject(ctx, ev, ReasonNASNotAllowed), nil
	}
	// The resolved instance's own service supersedes the bridge's coarse hint.
	hotspot := instance.Service == "hotspot"
	ev.Service = instance.Service
	ev.Instance = instanceName(instance)
	ev.Checks = append(ev.Checks, "service_instance")

	// 3. Resolve the read-model (needed by the service checks below).
	view, found, err := e.resolveView(ctx, req.Username)
	if err != nil {
		return authorizeResponse{}, err
	}
	// 3b. Hotspot voucher login (FR-18): the voucher code is posted as the
	// username. When it isn't a normal subscriber, try redeeming it as a voucher
	// via D's seam; success authorizes the session (the voucher is the
	// credential, so the password check is skipped).
	voucherAuthed := false
	if !found && hotspot {
		vview, ok, verr := e.tryVoucher(ctx, req.Username)
		if verr != nil {
			return authorizeResponse{}, verr
		}
		if ok {
			view, found, voucherAuthed = vview, true, true
			ev.Checks = append(ev.Checks, "voucher")
		}
	}
	if !found {
		return e.reject(ctx, ev, ReasonUnknownUser), nil
	}
	ev.Checks = append(ev.Checks, "user")

	// 4. Service-type matrix (FR-61 / C6 step 4). A redeemed voucher is
	// inherently a Hotspot credential, so it bypasses this gate (and step 5).
	//
	//   service_type | pppoe request | hotspot request
	//   pppoe        | accept-path   | service_not_allowed
	//   hotspot      | service_not_allowed | accept-path
	//   dual         | accept-path   | accept-path (FR-58)
	//
	// 'dual' is exactly v1's allow_hotspot=true and 'pppoe' its false, so every
	// migrated row decides identically to v1 (C1).
	if !voucherAuthed && !serviceTypeAllows(view.ServiceType, hotspot) {
		return e.reject(ctx, ev, ReasonServiceNotAllowed), nil
	}
	ev.Checks = append(ev.Checks, "service")

	// 5. NAS scope (FR-64 / C6 step 5) — after service-type, before credentials,
	// per the frozen chain. An account may be scoped to SEVERAL NAS/service
	// pairs; no scopes at all = any NAS (v1's behaviour). A voucher is not scoped
	// to a NAS, so it bypasses this too.
	if !voucherAuthed {
		if !scopeAllows(view.Scopes, nas.ID, instance.ID) {
			return e.reject(ctx, ev, ReasonNASNotAllowed), nil
		}
		ev.Checks = append(ev.Checks, "nas_scope")
	}

	// 6. Credentials — decryption happens ONLY here (NFR-4.2). Skipped for a
	// voucher login, which redemption already authenticated.
	if !voucherAuthed {
		plaintext, err := e.decrypt(view.PasswordEnc)
		if err != nil {
			return authorizeResponse{}, err
		}
		if !credentialsOK(string(plaintext), req) {
			return e.reject(ctx, ev, ReasonBadPassword), nil
		}
	}
	ev.Checks = append(ev.Checks, "credentials")

	// 7. Status disabled.
	if view.Status == "disabled" {
		return e.reject(ctx, ev, ReasonDisabled), nil
	}

	// 8. Expiry (FR-9). block → reject; expired_pool → walled-garden accept.
	// Applies to a hotspot-only subscriber exactly as it does to pppoe.
	mode := modeNormal
	if e.isExpired(view) {
		if view.ExpiryBehavior != "expired_pool" {
			return e.reject(ctx, ev, ReasonExpired), nil
		}
		mode = modeExpiredPool
		ev.Checks = append(ev.Checks, "expired_pool")
	} else if !quotaExempt(view.ServiceType, hotspot) && view.QuotaExhausted {
		// 9. Quota (FR-10 / C6 step 9). v1 skipped quota for EVERY hotspot
		// request; the exemption is really FR-58.3's "a dual subscriber's
		// hotspot leg is a bonus on top of their PPPoE plan", so it now applies
		// only to dual. A hotspot-ONLY subscriber's plan *is* their hotspot
		// usage, so their quota is enforced (FR-61.3).
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

	// 10. Simultaneous-session limit via C's live counter (FR-5/FR-58.2/FR-61.3).
	if rej, ok := e.sessionLimitReject(view, hotspot); ok {
		return e.reject(ctx, ev, rej), nil
	}
	ev.Checks = append(ev.Checks, "session_limit")

	// 11. MAC lock (FR-5) — PPPoE only; Hotspot devices are inherently different
	// MACs so the lock does not apply (task file).
	if !hotspot {
		if rej, ok := e.macLock(ctx, view, req.CallingStationID); ok {
			return e.reject(ctx, ev, rej), nil
		}
		ev.Checks = append(ev.Checks, "mac_lock")
	}

	// Accept: build the vendor-neutral reply intents for the chosen mode.
	resp := authorizeResponse{Action: "accept", Reason: ReasonOK, Attributes: e.replyIntents(view, instance, hotspot, mode)}
	ev.Outcome = "accept"
	ev.Reason = ReasonOK
	ev.Attributes = resp.Attributes
	e.record(ctx, ev)
	return resp, nil
}

// instanceName names the resolved instance for the debug tail, preferring what
// the operator will recognise. An unnamed sole instance (RouterOS allows a
// nameless PPPoE server) still reports its kind rather than an empty cell.
func instanceName(s serviceRow) string {
	if s.Label != "" {
		return s.Label
	}
	if s.ROSServerName != "" {
		return s.ROSServerName
	}
	return s.Service
}

// resolveInstance maps the request to one of the NAS's enabled service
// instances via the vendor adapter (C7 / FR-17: the attribute parsing that
// decides instance identity lives only in vendor/). false means the request
// cannot be attributed to an instance — no enabled instance of the requested
// kind exists on this NAS, or several do and none matched — which the caller
// turns into nas_not_allowed.
//
// It returns the full nas_services row rather than the adapter's ServiceInstance
// so the reply can use the instance's own pool (C5 sanctions passing the row
// straight to replyIntents). The row must be returned, never stashed on the
// engine: one engine serves every concurrent Access-Request.
func (e *engine) resolveInstance(ctx context.Context, nas nasIdentity, req authorizeRequest) (serviceRow, bool, error) {
	rows, err := e.servicesOf(ctx, nas.ID)
	if err != nil {
		return serviceRow{}, false, err
	}
	candidates := make([]vendor.ServiceInstance, 0, len(rows))
	byID := make(map[string]serviceRow, len(rows))
	for _, r := range rows {
		candidates = append(candidates, vendor.ServiceInstance{
			ID: r.ID, Service: r.Service, ROSServerName: r.ROSServerName,
		})
		byID[r.ID] = r
	}
	got, ok := vendor.For(nas.Vendor).ResolveService(vendor.ServiceQuery{
		Service:         req.Service,
		CalledStationID: req.CalledStationID,
		NASPortType:     req.NASPortType,
		NASPortID:       req.NASPortID,
	}, candidates)
	if !ok {
		return serviceRow{}, false, nil
	}
	return byID[got.ID], true, nil
}

// serviceTypeAllows is the FR-61 service-type matrix (C6 step 4). An unset
// service_type is treated as 'pppoe' — the column default and v1's meaning —
// so a view built before the field existed cannot accidentally allow hotspot.
func serviceTypeAllows(serviceType string, hotspot bool) bool {
	switch serviceType {
	case "dual":
		return true
	case "hotspot":
		return hotspot
	default: // "pppoe" and any unset/unknown value
		return !hotspot
	}
}

// quotaExempt reports whether the FR-58.3 quota exemption applies. Only a dual
// subscriber's hotspot leg is exempt: it is a bonus alongside their PPPoE plan.
// A hotspot-only subscriber's plan IS their hotspot usage, so quota applies
// (FR-61.3), and every pppoe request has always been subject to quota.
func quotaExempt(serviceType string, hotspot bool) bool {
	return hotspot && serviceType == "dual"
}

func (e *engine) isExpired(view AuthView) bool {
	if view.Status == "expired" {
		return true
	}
	return !view.ExpiresAt.IsZero() && e.now().After(view.ExpiresAt)
}

// sessionLimitReject applies the simultaneous-session limit (C6 step 10). The
// just-authorizing session is not yet in the live hash, so "already at the
// limit" means one more would exceed it.
//
// Three cases, by service type:
//   - dual + hotspot request: FR-58.2's separate allowance — exactly one
//     concurrent hotspot session, outside SessionLimit (v1 behaviour, unchanged).
//   - hotspot-only + hotspot request: hotspot IS the subscriber's service, so
//     hotspot sessions count against their own SessionLimit (FR-61.3). v1
//     applied the flat 1-session rule here, which would have made a 3-device
//     hotspot plan unsellable.
//   - pppoe request (pppoe or dual): count pppoe against SessionLimit, unchanged.
func (e *engine) sessionLimitReject(view AuthView, hotspot bool) (string, bool) {
	count := currentLiveCount()
	if hotspot {
		if view.ServiceType == "hotspot" {
			if view.SessionLimit > 0 && count(view.SubscriberID, "hotspot") >= view.SessionLimit {
				return ReasonSessionLimit, true
			}
			return "", false
		}
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

// replyIntents produces the abstract reply set for the outcome mode.
//
// Address precedence (FR-64.3, corrected 2026-07-16 after a pilot bug — see
// docs/ops/known-issues.md "no more free addresses in the pool") is
// SERVICE-AWARE:
//
//	pppoe request:   static_ip → the resolved pppoe instance's pool → the
//	                 profile's pool → omit.
//	hotspot request: static_ip → the resolved hotspot instance's pool → omit.
//
// The profile's pool is NEVER applied to a hotspot session. It is a PPPoE pool;
// v1 emitted it unconditionally, so the router tried to allocate from a named
// pool that usually didn't exist on it and the login failed with "no more free
// addresses in the pool". Omitting address_pool instead lets the MikroTik
// Hotspot assign from its own interface/DHCP pool — a hotspot router's normal
// behaviour. Locked by TestNoPoolOmitsAddressPool + the gate's item-6 legs.
//
// A static IP still wins over any pool: emit static_ip and omit the pool so the
// adapter maps it to Framed-IP-Address alone.
func (e *engine) replyIntents(view AuthView, instance serviceRow, hotspot bool, mode outcomeMode) []attribute {
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
		// Full-speed reply carries the profile's burst segments (FR-11); the
		// vendor adapter renders the concrete rate string so no burst syntax
		// leaks into the engine (FR-17). Non-empty burst fields only.
		add(IntentRateLimit, composeRate(view, rate))
	}

	// Address assignment (normal/throttled). Static IP takes precedence; then
	// the resolved service instance's own pool; then, for pppoe only, the
	// profile's pool. add() already skips an empty value, so "omit" is the
	// natural fallthrough.
	if view.StaticIP != "" {
		add(IntentStaticIP, view.StaticIP)
	} else if instance.IPPoolName != "" {
		add(IntentAddressPool, instance.IPPoolName)
	} else if !hotspot {
		add(IntentAddressPool, view.PoolName)
	}
	return attrs
}

// composeRate renders base (an abstract "rx/tx" rate) plus the view's optional
// burst segments (FR-11) into the rate string carried by the rate_limit intent.
// The MikroTik adapter owns the burst syntax (FR-17); when no burst fields are
// set it returns base unchanged.
func composeRate(view AuthView, base string) string {
	if base == "" {
		return ""
	}
	if view.BurstRate == "" && view.BurstThreshold == "" && view.BurstTime == "" &&
		view.RatePriority == "" && view.MinRate == "" {
		return base
	}
	return vendor.For("mikrotik").ComposeRate(vendor.RateSpec{
		Rate:           base,
		BurstRate:      view.BurstRate,
		BurstThreshold: view.BurstThreshold,
		BurstTime:      view.BurstTime,
		Priority:       view.RatePriority,
		MinRate:        view.MinRate,
	})
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
