package vendor

// ResolveService tests (C7): the MikroTik adapter is the only place that knows
// how a request identifies which hotspot/PPPoE instance it belongs to, so this
// is where that knowledge is pinned down.

import "testing"

func hs(id, name string) ServiceInstance {
	return ServiceInstance{ID: id, Service: "hotspot", ROSServerName: name}
}

func ppp(id, name string) ServiceInstance {
	return ServiceInstance{ID: id, Service: "pppoe", ROSServerName: name}
}

func TestResolveServiceByServerName(t *testing.T) {
	cands := []ServiceInstance{hs("lobby", "lobby"), hs("cafe", "cafe"), ppp("p", "")}
	got, ok := For("mikrotik").ResolveService(
		ServiceQuery{Service: "hotspot", CalledStationID: "cafe"}, cands)
	if !ok || got.ID != "cafe" {
		t.Fatalf("got %+v ok=%v, want the cafe instance", got, ok)
	}
}

// MikroTik server names are matched case-insensitively: the router's own
// spelling and the operator's typing in the panel need not agree exactly.
func TestResolveServiceServerNameCaseInsensitive(t *testing.T) {
	cands := []ServiceInstance{hs("lobby", "Lobby"), hs("cafe", "cafe")}
	got, ok := For("mikrotik").ResolveService(
		ServiceQuery{Service: "hotspot", CalledStationID: "LOBBY"}, cands)
	if !ok || got.ID != "lobby" {
		t.Fatalf("got %+v ok=%v, want the lobby instance", got, ok)
	}
}

// Some builds append the AP MAC to the server name; only the first token is the
// name.
func TestResolveServiceStripsAPMACSuffix(t *testing.T) {
	cands := []ServiceInstance{hs("lobby", "lobby"), hs("cafe", "cafe")}
	got, ok := For("mikrotik").ResolveService(
		ServiceQuery{Service: "hotspot", CalledStationID: "lobby:AA:BB:CC:DD:EE:FF"}, cands)
	if !ok || got.ID != "lobby" {
		t.Fatalf("got %+v ok=%v, want the lobby instance", got, ok)
	}
}

// A NAS running exactly one instance of the requested kind is unambiguous even
// with no name to match — this is the single-service NAS every v1 install
// upgrades into, and why v1 auth behaviour is unchanged after the migration.
func TestResolveServiceSingleInstanceNeedsNoName(t *testing.T) {
	cands := []ServiceInstance{hs("only", ""), ppp("p", "")}
	got, ok := For("mikrotik").ResolveService(ServiceQuery{Service: "hotspot"}, cands)
	if !ok || got.ID != "only" {
		t.Fatalf("got %+v ok=%v, want the sole hotspot instance", got, ok)
	}
}

// A bare MAC in Called-Station-Id means the router sent no server name. It must
// not be matched against a name — and with one candidate it still resolves.
func TestResolveServiceBareMACIsNotAName(t *testing.T) {
	cands := []ServiceInstance{hs("only", "hotspot1")}
	got, ok := For("mikrotik").ResolveService(
		ServiceQuery{Service: "hotspot", CalledStationID: "AA:BB:CC:DD:EE:FF"}, cands)
	if !ok || got.ID != "only" {
		t.Fatalf("got %+v ok=%v, want fallback to the sole instance", got, ok)
	}
}

// Several instances and no match = ambiguous. Returning false is what makes the
// engine reject instead of handing the session another zone's pool.
func TestResolveServiceAmbiguousReturnsFalse(t *testing.T) {
	cands := []ServiceInstance{hs("lobby", "lobby"), hs("cafe", "cafe")}
	if got, ok := For("mikrotik").ResolveService(
		ServiceQuery{Service: "hotspot", CalledStationID: "garden"}, cands); ok {
		t.Fatalf("resolved %+v; an unmatched name among several instances is ambiguous", got)
	}
}

// No instance of the requested kind: the C6-step-2 reject the engine maps to
// nas_not_allowed.
func TestResolveServiceNoInstanceOfKind(t *testing.T) {
	cands := []ServiceInstance{ppp("p", "")}
	if _, ok := For("mikrotik").ResolveService(ServiceQuery{Service: "hotspot"}, cands); ok {
		t.Fatal("resolved a hotspot request on a NAS with no hotspot instance")
	}
	if _, ok := For("mikrotik").ResolveService(ServiceQuery{Service: "pppoe"}, nil); ok {
		t.Fatal("resolved against an empty candidate set")
	}
}

// A hotspot request must never resolve to a PPPoE instance (or vice versa) —
// the kinds partition the candidate set before any name matching.
func TestResolveServiceNeverCrossesKind(t *testing.T) {
	cands := []ServiceInstance{ppp("p", "shared-name"), hs("h", "other")}
	got, ok := For("mikrotik").ResolveService(
		ServiceQuery{Service: "hotspot", CalledStationID: "shared-name"}, cands)
	if !ok || got.ID != "h" {
		t.Fatalf("got %+v ok=%v; a hotspot request must resolve within hotspot instances only", got, ok)
	}
}
