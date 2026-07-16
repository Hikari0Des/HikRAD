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

// --- unknown coarse kind (the accounting case) -----------------------------

// The pilot bug, reproduced (2026-07-16): a MikroTik omits Service-Type from
// Accounting-Requests, so the bridge's coarse hint is EMPTY. Filtering by a
// guessed "pppoe" found no candidates on this hotspot-only NAS and gave up
// before ever looking at Called-Station-Id — filing every hotspot session as
// pppoe in the panel. An empty kind must identify by attributes instead.
func TestResolveService_UnknownKindMatchesByStationName(t *testing.T) {
	candidates := []ServiceInstance{
		{ID: "svc-warith", Service: "hotspot", ROSServerName: "Warith-Wifi"},
		{ID: "svc-free", Service: "hotspot", ROSServerName: "Warith-Free"},
		{ID: "svc-students", Service: "hotspot", ROSServerName: "Students-Wifi"},
	}
	got, ok := For("mikrotik").ResolveService(ServiceQuery{
		Service:         "", // the NAS sent no Service-Type
		CalledStationID: "Students-Wifi",
	}, candidates)
	if !ok {
		t.Fatal("an unknown kind failed to resolve despite an exact server-name match")
	}
	if got.ID != "svc-students" {
		t.Errorf("resolved to %+v, want svc-students", got)
	}
	// And the instance's own service is the answer — not the missing hint.
	if got.Service != "hotspot" {
		t.Errorf("resolved service = %q, want hotspot", got.Service)
	}
}

// An unknown kind on a NAS with exactly one instance is unambiguous.
func TestResolveService_UnknownKindSoleInstance(t *testing.T) {
	got, ok := For("mikrotik").ResolveService(ServiceQuery{Service: ""},
		[]ServiceInstance{{ID: "svc-hs", Service: "hotspot", ROSServerName: "lobby"}})
	if !ok || got.ID != "svc-hs" {
		t.Fatalf("got %+v ok=%v, want svc-hs", got, ok)
	}
}

// An unknown kind with nothing to disambiguate stays ambiguous — the resolver
// must not pick a zone at random.
func TestResolveService_UnknownKindAmbiguousStillFails(t *testing.T) {
	if _, ok := For("mikrotik").ResolveService(ServiceQuery{Service: ""}, []ServiceInstance{
		{ID: "a", Service: "hotspot", ROSServerName: "lobby"},
		{ID: "b", Service: "hotspot", ROSServerName: "cafe"},
	}); ok {
		t.Fatal("an unnamed request among several zones resolved; it must stay ambiguous")
	}
}

// A DEFINITE kind still filters — C6 step 2's nas_not_allowed depends on it, so
// a pppoe request on a hotspot-only NAS must not start matching hotspot zones.
func TestResolveService_DefiniteKindStillFilters(t *testing.T) {
	if _, ok := For("mikrotik").ResolveService(ServiceQuery{
		Service:         "pppoe",
		CalledStationID: "Students-Wifi",
	}, []ServiceInstance{{ID: "svc-students", Service: "hotspot", ROSServerName: "Students-Wifi"}}); ok {
		t.Fatal("a pppoe request matched a hotspot instance; the kind filter must still apply")
	}
}
