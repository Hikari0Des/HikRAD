package live

// Live-session filtering + manager scoping (contract C6, FR-31.3). Kept as a
// pure function over resolved subscriber attributes so it is unit-testable
// without Redis or a DB, and so the SSE snapshot, the SSE delta stream and the
// List interface all apply exactly the same rules.

import (
	"strings"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/live/livestate"
)

// Filter is the set of query params the live feed / List accept (C6/C7).
type Filter struct {
	NASID     string
	ProfileID string
	ManagerID string
	Q         string
	// Service (FR-61/63) filters by the SESSION's own service (pppoe|hotspot),
	// not the subscriber's service_type: a dual subscriber has both kinds of
	// session, and "show me the hotspot sessions" is the question this answers.
	Service string
}

// subjectAttrs are the subscriber attributes needed for profile/manager
// filtering and scope enforcement, resolved from subscriber_id. Empty owner
// means "unknown" (e.g. a session with no matched subscriber, or before D's
// owner_manager_id column lands) — a scoped manager must NOT see those.
type subjectAttrs struct {
	ProfileID      string
	OwnerManagerID string
}

// matchState reports whether a live session passes the filter and the caller's
// scope. scope nil = unscoped (admin/operator sees all owners).
func matchState(s livestate.State, f Filter, scope *auth.ManagerScope, attrs subjectAttrs) bool {
	if f.NASID != "" && s.NASID != f.NASID {
		return false
	}
	if f.ProfileID != "" && attrs.ProfileID != f.ProfileID {
		return false
	}
	if f.Service != "" && s.Service != f.Service {
		return false
	}
	if f.ManagerID != "" && attrs.OwnerManagerID != f.ManagerID {
		return false
	}
	if scope != nil && attrs.OwnerManagerID != scope.ManagerID {
		// Deny-by-default: a scoped agent never sees a session whose owner is
		// unknown or not theirs (FR-27.2 server-side enforcement).
		return false
	}
	if f.Q != "" {
		q := strings.ToLower(f.Q)
		if !strings.Contains(strings.ToLower(s.Username), q) &&
			!strings.Contains(strings.ToLower(s.IP), q) &&
			!strings.Contains(strings.ToLower(s.MAC), q) {
			return false
		}
	}
	return true
}
