package portalapi

// Subscriber identity middleware (contract C2 IDOR rule: identity comes ONLY
// from the token; no subscriber_id ever appears as a route/body param on any
// portal endpoint in this package).

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/hikrad/hikrad/internal/httpapi"
)

// Subscriber is the authenticated caller for the duration of a request.
type Subscriber struct {
	ID        string
	SessionID string
	IP        string
	UA        string
	// Scope is scopeFull or scopeReduced (v4-11, FR-167), read from the
	// access token's own "scp" claim — the same self-contained-token model
	// requireSubscriber already uses for sub/sid (no per-request DB hit on
	// the hot path). requireFullScope below is what actually enforces it.
	Scope string
}

type subscriberCtxKey struct{}

func withSubscriber(ctx context.Context, s *Subscriber) context.Context {
	return context.WithValue(ctx, subscriberCtxKey{}, s)
}

// SubscriberFrom returns the authenticated Subscriber, if requireSubscriber ran.
func SubscriberFrom(ctx context.Context) (*Subscriber, bool) {
	s, ok := ctx.Value(subscriberCtxKey{}).(*Subscriber)
	return s, ok
}

// requireSubscriber authenticates the portal access token. A missing/invalid
// token, or a valid PANEL token (different "typ" claim — see tokens.go), is a
// 401: audience separation is a byproduct of the type check, not a lookup.
func (m *Module) requireSubscriber(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
			return
		}
		sub, sid, scope, err := parseAccess(m.jwtSecret, raw)
		if err != nil {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
			return
		}
		s := &Subscriber{ID: sub, SessionID: sid, IP: clientIP(r), UA: r.UserAgent(), Scope: scope}
		next.ServeHTTP(w, r.WithContext(withSubscriber(r.Context(), s)))
	})
}

// requireFullScope wraps a handler that changes password, contact details,
// notification-channel preferences, or payment configuration — the frozen
// contract (phase brief §4): "Scope is enforced server-side on every
// endpoint. A reduced-scope token calling a full-scope endpoint gets 403
// regardless of what the UI offered." Must run AFTER requireSubscriber (it
// reads the Subscriber already placed in context) and is a 403, not a 401 —
// the caller IS authenticated, just not for this action; the response code
// tells the portal client to offer the password step-up prompt rather than a
// full re-login.
func (m *Module) requireFullScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, ok := SubscriberFrom(r.Context())
		if !ok {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
			return
		}
		if sub.Scope != scopeFull {
			httpapi.Error(w, http.StatusForbidden, "step_up_required",
				"this action requires your password; the auto-signed-in session is view/renew/pay only")
			return
		}
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || raw == "" {
		return "", false
	}
	return raw, true
}

// clientIP mirrors internal/auth's: first X-Forwarded-For hop (Caddy, NFR-4.4)
// else the transport remote address, port stripped.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); first != "" {
			return first
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
