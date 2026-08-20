package portalapi

// Connected-client auto-login (FR-167, Decision 64) and its captive-portal
// hand-off (FR-168). BUILD THIS AS A THREAT MODEL FIRST: it is the one path
// in the whole portal API that authenticates a subscriber WITHOUT a
// credential, on the strength of "this HTTP request's true source IP matches
// exactly one currently-live RADIUS session". Every condition below is
// REQUIRED — this file must never grow a "pick the most likely session"
// fallback, a wider XFF-trust default, or a second, looser implementation for
// the captive-portal case (phase brief §3.2/§4, binding).
//
// The six conditions, in the order this file checks them (fail-closed, first
// failure wins — no condition is ever skipped because an earlier one looked
// close enough):
//
//  1. Feature enabled for the instance (off by default).
//  2. The request's TRUE source IP (see resolveSourceIP; never the raw
//     X-Forwarded-For header) falls inside an operator-configured trusted
//     network.
//  3. X-Forwarded-For is honoured ONLY when the immediate transport peer is
//     itself a configured trusted proxy — resolveSourceIP's whole job.
//  4. That IP matches EXACTLY ONE live session (livestate.ByIP). Zero
//     matches and two-or-more matches are both refusals; there is no
//     tie-break.
//  5. If the caller supplied a MAC hint (always true for the captive-portal
//     hand-off, since RouterOS's login-page redirect carries $(mac); never
//     present for a plain portal page load), it must match the matched
//     session's own MAC as recorded by RADIUS accounting — a hint that
//     disagrees with the session refuses, it never overrides the session.
//  6. The matched subscriber's status is "active" or "expired" — anything
//     else (most notably "disabled") refuses even though password login
//     would still allow it: a manager who disabled an account made a
//     deliberate decision this credential-free path must not paper over.
//
// A supplied IP hint (FR-168's captive-portal ip= param) is NEVER used to
// pick the trusted network or the live session — conditions 2-4 always run
// against the request's real resolved source IP. The hint is checked ONLY as
// an additional corroboration on the already-matched session (same
// treatment as the MAC hint), because "hints, validated against the
// live-session table, never trusted on their own" applies to IP exactly as
// much as to MAC.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/subscribers"
)

// Refusal reason codes — every one of these is written to the audit log
// (gate item 12) exactly as-is, so an operator (or an alert rule built on
// audit_log) can query by reason. "ambiguous_ip" is the one gate item 7
// specifically calls out as the shape of an attack; the phrase appears
// verbatim in the audit row precisely so that query is trivial to write.
const (
	reasonDisabled          = "disabled"
	reasonUntrustedNetwork  = "untrusted_network"
	reasonNoSession         = "no_session"
	reasonAmbiguousIP       = "ambiguous_ip"
	reasonMACMismatch       = "mac_mismatch"
	reasonIPHintMismatch    = "ip_hint_mismatch"
	reasonInactive          = "inactive"
)

// autoLoginConfig is the portal_auto_login.* settings group (migration
// 0631). See loadAutoLoginConfig's doc comment for the fail-closed defaults.
type autoLoginConfig struct {
	Enabled           bool
	TrustedNetworks   []string
	TrustedProxies    []string
	SessionTTLMinutes int
}

// loadAutoLoginConfig reads the four settings keys migration 0631 seeds on
// every install. It tolerates a missing or undecodable key by defaulting to
// the SAFE-CLOSED value for that one field (enabled=false, an empty CIDR
// list) rather than failing the whole load — a partially-corrupt config can
// only ever narrow this path, never widen it into "unrestricted".
func loadAutoLoginConfig(ctx context.Context, s platform.Settings) autoLoginConfig {
	cfg := autoLoginConfig{SessionTTLMinutes: 30}
	if v, err := platform.Get[bool](ctx, s, "portal_auto_login.enabled"); err == nil {
		cfg.Enabled = v
	}
	if v, err := platform.Get[[]string](ctx, s, "portal_auto_login.trusted_networks"); err == nil {
		cfg.TrustedNetworks = v
	}
	if v, err := platform.Get[[]string](ctx, s, "portal_auto_login.trusted_proxies"); err == nil {
		cfg.TrustedProxies = v
	}
	if v, err := platform.Get[int](ctx, s, "portal_auto_login.session_ttl_minutes"); err == nil && v > 0 {
		cfg.SessionTTLMinutes = v
	}
	return cfg
}

// ipInNetworks reports whether ip falls inside any CIDR in networks.
// Deliberately the INVERSE default of internal/auth's ipAllowed (manager IP
// allowlist): there, an empty list means unrestricted, because a manager
// allowlist is opt-in narrowing of an otherwise-open door. Here the door is
// closed to begin with (a credential-free login), so an empty or unparseable
// configuration must mean "trust nothing", never "trust everything" — this
// is the off-by-default posture gate item 4 requires, expressed at the
// lowest level so no caller can get it backwards. A malformed CIDR entry
// simply never matches (fails closed per-entry, same discipline ipAllowed
// already uses for that half).
func ipInNetworks(ip string, networks []string) bool {
	if ip == "" || len(networks) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, c := range networks {
		if _, network, err := net.ParseCIDR(c); err == nil && network.Contains(parsed) {
			return true
		}
	}
	return false
}

// resolveSourceIP returns the request's TRUE origin IP for THIS decision.
// It deliberately does NOT call the package-wide clientIP() (middleware.go),
// which unconditionally trusts the first X-Forwarded-For hop under the
// single-ingress-proxy (Caddy) assumption every other portal endpoint
// relies on (NFR-4.4). That assumption is exactly the gap condition 3 exists
// to close: here, XFF is honoured ONLY when the request's own immediate
// transport peer is itself inside a configured trusted-proxy network.
// Anyone who can reach the API directly, or through anything not on that
// list, gets their raw transport-layer address used — so a forged
// X-Forwarded-For header cannot substitute a subscriber's IP for an
// attacker's.
func resolveSourceIP(r *http.Request, trustedProxies []string) string {
	peer := hostOnly(r.RemoteAddr)
	if !ipInNetworks(peer, trustedProxies) {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	if first == "" {
		return peer
	}
	return first
}

func hostOnly(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// normalizeMAC mirrors internal/radius/policy.go's unexported normalizeMAC
// (upper-case, strip separators, keep only the first whitespace-delimited
// token) — that function is a private implementation detail of package
// radius (not importable), so this is its own small copy, same convention
// portalapi/ratelimit.go already established for internal/auth's limiter.
func normalizeMAC(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	r := strings.NewReplacer(":", "", "-", "", ".", "")
	return strings.ToUpper(r.Replace(s))
}

// autoLoginHints is the optional POST body: absent/empty for a plain portal
// page load (FR-167), populated from the hotspot redirect's own MAC/IP
// parameters for the captive-portal hand-off (FR-168) — the SAME endpoint,
// SAME evaluation function, for both; there is no second, looser path.
type autoLoginHints struct {
	MAC string `json:"mac,omitempty"`
	IP  string `json:"ip,omitempty"`
}

func parseAutoLoginHints(r *http.Request) autoLoginHints {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	var h autoLoginHints
	if len(strings.TrimSpace(string(body))) == 0 {
		return h
	}
	_ = json.Unmarshal(body, &h) // malformed/absent JSON just means no hints, never an error
	return h
}

// autoLoginOutcome is evaluateAutoLogin's result — always carries enough to
// audit (gate item 12), whether granted or refused.
type autoLoginOutcome struct {
	granted      bool
	reason       string // "" when granted
	sourceIP     string
	hintMAC      string
	subscriberID string // "" when no session ever matched
	sessionMAC   string
	nasID        string
	sessionField string // live:sessions hash field, "" when none matched
	status       string // subscriber status, "" when no session ever matched
}

// evaluateAutoLogin runs conditions 1-6 in order, fail-closed. It is the
// SINGLE implementation both the plain auto-login call and the
// captive-portal hand-off run through (FR-168's binding "no second
// implementation" instruction) — hints are simply empty in the plain case.
func evaluateAutoLogin(ctx context.Context, m *Module, r *http.Request, hints autoLoginHints) autoLoginOutcome {
	cfg := loadAutoLoginConfig(ctx, m.settings)

	// Condition 1.
	if !cfg.Enabled {
		return autoLoginOutcome{reason: reasonDisabled}
	}

	// Conditions 2/3: resolve the TRUE source IP (trusted-proxy-aware), then
	// require it inside the trusted-network allowlist.
	sourceIP := resolveSourceIP(r, cfg.TrustedProxies)
	out := autoLoginOutcome{sourceIP: sourceIP, hintMAC: hints.MAC}
	if !ipInNetworks(sourceIP, cfg.TrustedNetworks) {
		out.reason = reasonUntrustedNetwork
		return out
	}

	// Condition 4: exactly one live session on that IP.
	sessions, err := livestate.ByIP(ctx, m.rdb, sourceIP)
	if err != nil {
		out.reason = reasonNoSession
		return out
	}
	if len(sessions) == 0 {
		out.reason = reasonNoSession
		return out
	}
	if len(sessions) > 1 {
		out.reason = reasonAmbiguousIP
		return out
	}
	session := sessions[0]
	out.subscriberID = session.SubscriberID
	out.sessionMAC = session.MAC
	out.nasID = session.NASID
	out.sessionField = livestate.Field(session.NASID, session.AcctSessionID)
	if session.SubscriberID == "" {
		// A live session the pipeline could not attribute to a subscriber
		// (recorded, never dropped — M2) can never auto-login anyone; there
		// is no identity to grant.
		out.reason = reasonNoSession
		return out
	}

	// Condition 5: a supplied MAC hint must corroborate, never override.
	if hints.MAC != "" {
		if session.MAC == "" || normalizeMAC(session.MAC) != normalizeMAC(hints.MAC) {
			out.reason = reasonMACMismatch
			return out
		}
	}
	// Same treatment for a supplied IP hint (FR-168): it must match the
	// request's own resolved source IP that the session was actually found
	// at — never trusted on its own to select or override anything.
	if hints.IP != "" && hints.IP != sourceIP {
		out.reason = reasonIPHintMismatch
		return out
	}

	// Condition 6: active or expired only — a manager-disabled (or any other
	// non-{active,expired}) account never gets the credential-free path,
	// even though password login still allows it (login.go's own rule).
	status, err := subscriberStatus(ctx, m, session.SubscriberID)
	if err != nil {
		out.reason = reasonNoSession
		return out
	}
	out.status = status
	if status != "active" && status != "expired" {
		out.reason = reasonInactive
		return out
	}

	out.granted = true
	return out
}

func subscriberStatus(ctx context.Context, m *Module, subscriberID string) (string, error) {
	var status string
	err := m.db.QueryRow(ctx, `SELECT status FROM subscribers WHERE id = $1::uuid`, subscriberID).Scan(&status)
	return status, err
}

// auditAutoLogin writes gate item 12's row: IP, MAC, NAS, session id, scope
// — granted or refused, every single time, through the SAME append-only
// audit_log every other mutating/security event in the product uses
// (auth.AuditActor) rather than a bespoke table, so the existing panel Audit
// Log page (filterable by action) is already the "make a refusal spike
// visible" surface with zero new UI. A burst of action=portal.autologin.refused
// reason=ambiguous_ip rows in a short window IS the attack signature the
// phase brief names explicitly.
func auditAutoLogin(ctx context.Context, out autoLoginOutcome, scope, origin string) {
	action := "portal.autologin.refused"
	entityID := out.subscriberID
	detail := map[string]any{
		"ip":       out.sourceIP,
		"mac":      firstNonEmpty(out.hintMAC, out.sessionMAC),
		"nas_id":   out.nasID,
		"session":  out.sessionField,
		"reason":   out.reason,
		"origin":   origin,
	}
	if out.granted {
		action = "portal.autologin.granted"
		detail["scope"] = scope
		delete(detail, "reason")
	}
	_ = auth.AuditActor(ctx, entityID, out.sourceIP, "", action, "subscriber", entityID, nil, detail)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// autoLoginHandler serves POST /api/v1/portal/session/auto (FR-167 §4's
// frozen contract; FR-168 reuses it verbatim for the captive-portal hand-off
// — this file's evaluateAutoLogin is the one code path for both). A refusal
// is an ordinary, EXPECTED outcome for most page loads (no live session,
// feature off, etc.) — it is reported as 401 with a machine-readable code so
// the portal client falls back to the normal login screen silently, never as
// a user-facing error.
func (m *Module) autoLoginHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hints := parseAutoLoginHints(r)
	origin := "auto"
	if hints.MAC != "" || hints.IP != "" {
		origin = "captive"
	}

	out := evaluateAutoLogin(ctx, m, r, hints)
	if !out.granted {
		auditAutoLogin(ctx, out, "", origin)
		httpapi.Error(w, http.StatusUnauthorized, "auto_login_refused", "auto_login_refused")
		return
	}

	id, err := subscribers.LoadIdentity(ctx, m.db, out.subscriberID)
	if err != nil {
		m.internalError(w, "auto-login load identity", err)
		return
	}

	cfg := loadAutoLoginConfig(ctx, m.settings)
	hardCap := time.Now().Add(time.Duration(cfg.SessionTTLMinutes) * time.Minute)

	secret, hash, err := newRefreshSecret()
	if err != nil {
		m.internalError(w, "auto-login", err)
		return
	}
	sessionID, err := createSession(ctx, m.db, id.ID, hash, out.sourceIP, r.UserAgent(), scopeReduced, &hardCap)
	if err != nil {
		m.internalError(w, "auto-login create session", err)
		return
	}
	access, err := issueAccess(m.jwtSecret, id.ID, sessionID, scopeReduced, time.Now(), &hardCap)
	if err != nil {
		m.internalError(w, "auto-login issue token", err)
		return
	}

	auditAutoLogin(ctx, out, scopeReduced, origin)

	httpapi.JSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		RefreshToken: composeRefreshToken(sessionID, secret),
		Subscriber:   subscriberBrief{ID: id.ID, Username: id.Username, Name: id.Name, Language: id.Language},
		Scope:        scopeReduced,
	})
}

// --- password step-up (FR-167: "the portal prompts, the subscriber types
// their password once, and the session upgrades to full for its remaining
// lifetime") --------------------------------------------------------------

type stepUpRequest struct {
	Password string `json:"password" validate:"required"`
}

// stepUpHandler serves POST /api/v1/portal/session/step-up. Requires an
// already-authenticated portal token (any scope — a full-scope session
// calling this is a harmless no-op) plus the real password, verified through
// the exact same subscribers.VerifyPasswordByID path a normal login uses.
// On success the EXISTING session row is promoted in place — same id, same
// hard cap if it had one (upgradeSessionScope never touches expires_at) —
// so "for its remaining lifetime" is exact, not a fresh unbounded grant.
func (m *Module) stepUpHandler(w http.ResponseWriter, r *http.Request) {
	sub, ok := SubscriberFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	var in stepUpRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	ctx := r.Context()

	ok, err := subscribers.VerifyPasswordByID(ctx, m.db, sub.ID, in.Password)
	if err != nil {
		m.internalError(w, "step-up verify", err)
		return
	}
	if !ok {
		_ = auth.AuditActor(ctx, sub.ID, sub.IP, sub.UA, "portal.autologin.stepup_failed", "portal_session", sub.SessionID, nil, nil)
		httpapi.Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid password")
		return
	}

	scope, hardCap, revoked, err := sessionScope(ctx, m.db, sub.SessionID)
	if err != nil || revoked {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}
	if scope != scopeFull {
		if err := upgradeSessionScope(ctx, m.db, sub.SessionID); err != nil {
			m.internalError(w, "step-up upgrade", err)
			return
		}
		_ = auth.AuditActor(ctx, sub.ID, sub.IP, sub.UA, "portal.autologin.stepup", "portal_session", sub.SessionID, nil,
			map[string]any{"from_scope": scope, "to_scope": scopeFull})
	}

	access, err := issueAccess(m.jwtSecret, sub.ID, sub.SessionID, scopeFull, time.Now(), hardCap)
	if err != nil {
		m.internalError(w, "step-up issue token", err)
		return
	}
	id, err := subscribers.LoadIdentity(ctx, m.db, sub.ID)
	if err != nil {
		m.internalError(w, "step-up load identity", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, tokenResponse{
		AccessToken: access,
		Subscriber:  subscriberBrief{ID: id.ID, Username: id.Username, Name: id.Name, Language: id.Language},
		Scope:       scopeFull,
	})
}
