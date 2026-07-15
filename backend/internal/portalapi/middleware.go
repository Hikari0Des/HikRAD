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
		sub, sid, err := parseAccess(m.jwtSecret, raw)
		if err != nil {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
			return
		}
		s := &Subscriber{ID: sub, SessionID: sid, IP: clientIP(r), UA: r.UserAgent()}
		next.ServeHTTP(w, r.WithContext(withSubscriber(r.Context(), s)))
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
