package live

import (
	"testing"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/live/livestate"
)

func state(sub, nas, user, ip, mac string) livestate.State {
	return livestate.State{SubscriberID: sub, NASID: nas, Username: user, IP: ip, MAC: mac, Service: livestate.ServicePPPoE}
}

func TestMatchStateFilters(t *testing.T) {
	s := state("sub-1", "nas-1", "noor", "10.0.0.9", "AA:BB:CC:00:11:22")
	attrs := subjectAttrs{ProfileID: "prof-1", OwnerManagerID: "mgr-1"}

	cases := []struct {
		name  string
		f     Filter
		scope *auth.ManagerScope
		attrs subjectAttrs
		want  bool
	}{
		{"no filter", Filter{}, nil, attrs, true},
		{"nas match", Filter{NASID: "nas-1"}, nil, attrs, true},
		{"nas miss", Filter{NASID: "nas-2"}, nil, attrs, false},
		{"profile match", Filter{ProfileID: "prof-1"}, nil, attrs, true},
		{"profile miss", Filter{ProfileID: "prof-9"}, nil, attrs, false},
		{"manager match", Filter{ManagerID: "mgr-1"}, nil, attrs, true},
		{"manager miss", Filter{ManagerID: "mgr-9"}, nil, attrs, false},
		{"q username", Filter{Q: "NOO"}, nil, attrs, true},
		{"q ip", Filter{Q: "0.0.9"}, nil, attrs, true},
		{"q mac", Filter{Q: "aa:bb"}, nil, attrs, true},
		{"q miss", Filter{Q: "zzz"}, nil, attrs, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchState(s, c.f, c.scope, c.attrs); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestMatchStateScope(t *testing.T) {
	s := state("sub-1", "nas-1", "noor", "10.0.0.9", "AA:BB")

	// Scoped to the owner → visible.
	owned := subjectAttrs{OwnerManagerID: "mgr-1"}
	if !matchState(s, Filter{}, &auth.ManagerScope{ManagerID: "mgr-1"}, owned) {
		t.Fatal("owner should see their own session")
	}
	// Scoped to a different manager → hidden.
	if matchState(s, Filter{}, &auth.ManagerScope{ManagerID: "mgr-2"}, owned) {
		t.Fatal("a scoped manager must not see another manager's session")
	}
	// Scoped but owner unknown (pre-D, or unmatched subscriber) → hidden (deny).
	if matchState(s, Filter{}, &auth.ManagerScope{ManagerID: "mgr-1"}, subjectAttrs{}) {
		t.Fatal("unknown owner must be hidden from a scoped manager")
	}
}
