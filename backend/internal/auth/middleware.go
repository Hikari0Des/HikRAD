package auth

// Request-scoped identity + authorization middleware (contract C2). Every
// Phase-2 API module adopts these:
//
//	r.With(auth.Require("subscribers.view")).Get(...)      // authn + authz
//	scope := auth.ScopeFilter(r.Context())                 // row ownership
//	m, _ := auth.ManagerFrom(r.Context())                  // actor for Audit
//
// The rich Manager lives in this package's context (httpapi.Identity is only
// {ManagerID, Role} and cannot be extended from here), populated straight from
// the access-token claims so authorization needs no database round-trip.

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/hikrad/hikrad/internal/httpapi"
)

// Manager is the authenticated caller for the duration of a request.
type Manager struct {
	ID        string
	Role      string
	Scoped    bool
	SessionID string
	IP        string
	UA        string
	// Perms is the resolved effective permission set from the access token
	// (FR-27). When nil (a Manager built without a resolved set, e.g. in unit
	// tests), Can falls back to the builtin role map.
	Perms map[string]bool
	// AllowedIPs is the resolved CIDR allowlist from the token (FR-30); empty
	// means unrestricted.
	AllowedIPs []string
}

// Can reports whether the manager holds a permission string (contract C2/C7).
// The embedded resolved set is authoritative when present (wildcard '*' grants
// all); otherwise it falls back to the builtin role map so a Manager
// constructed without a resolved set (unit tests) preserves Phase-2 semantics.
func (m *Manager) Can(perm string) bool {
	if m.Perms != nil {
		return m.Perms[permWildcard] || m.Perms[perm]
	}
	return roleCan(m.Role, perm)
}

// ManagerScope is the ownership filter returned by ScopeFilter.
type ManagerScope struct {
	// ManagerID is the owner every subscriber-owned row must match.
	ManagerID string
}

type managerCtxKey struct{}

func withManager(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, managerCtxKey{}, m)
}

// ManagerFrom returns the authenticated Manager, if Require ran on this route.
func ManagerFrom(ctx context.Context) (*Manager, bool) {
	m, ok := ctx.Value(managerCtxKey{}).(*Manager)
	return m, ok
}

// ScopeFilter returns the ownership filter to apply to every list/get/mutation
// on subscriber-owned data (FR-27.2), or nil for an unscoped (admin) caller or
// an unauthenticated context. Callers MUST apply it server-side, never rely on
// UI hiding.
func ScopeFilter(ctx context.Context) *ManagerScope {
	m, ok := ManagerFrom(ctx)
	if !ok || !m.Scoped {
		return nil
	}
	return &ManagerScope{ManagerID: m.ID}
}

// Require builds middleware that authenticates the bearer access token and,
// when perm != "", enforces it (deny-by-default). It stores the Manager in the
// request context. A missing/invalid token is a 401; an authenticated caller
// lacking the permission is a 403 that is audited (FR-27, AC-27a).
func Require(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc == nil {
				httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "authentication is not configured")
				return
			}
			m, err := managerFromRequest(r)
			if err != nil {
				httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
				return
			}
			ctx := withManager(r.Context(), m)
			// IP allowlist (FR-30): enforced on every request against the set
			// embedded in the token. Empty list = unrestricted.
			if !ipAllowed(m.IP, m.AllowedIPs) {
				_ = Audit(ctx, "auth.ip_denied", "manager", m.ID, nil, ipDeniedDetail{IP: m.IP})
				httpapi.Error(w, http.StatusForbidden, "ip_not_allowed", "your network is not permitted for this account")
				return
			}
			if perm != "" && !m.Can(perm) {
				// Denials are audited (AC-27a) with the actor already in ctx.
				_ = Audit(ctx, "auth.denied", "permission", perm, nil, deniedDetail{Permission: perm, Method: r.Method, Path: r.URL.Path})
				httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have permission to perform this action")
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type deniedDetail struct {
	Permission string `json:"permission"`
	Method     string `json:"method"`
	Path       string `json:"path"`
}

type ipDeniedDetail struct {
	IP string `json:"ip"`
}

// managerFromRequest parses and validates the access token into a Manager.
func managerFromRequest(r *http.Request) (*Manager, error) {
	raw, ok := bearerToken(r)
	if !ok {
		return nil, errBadToken
	}
	claims, err := svc.tokens.parseAccess(raw)
	if err != nil {
		return nil, err
	}
	perms := make(map[string]bool, len(claims.Perms))
	for _, p := range claims.Perms {
		perms[p] = true
	}
	return &Manager{
		ID:         claims.ManagerID,
		Role:       claims.Role,
		Scoped:     claims.Scoped,
		SessionID:  claims.SessionID,
		IP:         clientIP(r),
		UA:         r.UserAgent(),
		Perms:      perms,
		AllowedIPs: claims.AllowedIPs,
	}, nil
}

func bearerToken(r *http.Request) (string, bool) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || raw == "" {
		return "", false
	}
	return raw, true
}

// clientIP is the best-effort client address: the first X-Forwarded-For hop
// (Caddy sets it, NFR-4.4) else the transport remote address, port stripped.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if first != "" {
			return first
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
