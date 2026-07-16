package accounting

// Service resolution for accounting records (FR-62.5). These run with no DB:
// nasInfo.resolveService is pure once the NAS's instances are known.
//
// The case that matters is the pilot's (2026-07-16): a MikroTik sends
// Service-Type=Login-User on a hotspot Access-Request but OMITS it from
// Accounting-Requests. The bridge's coarse hint therefore arrives empty, and
// treating empty as "pppoe" filed every hotspot session as pppoe — the panel
// showed "PPPoE" next to a hotspot user's name.

import (
	"testing"

	"github.com/hikrad/hikrad/internal/radius/vendor"
)

// pilotNAS mirrors the real pilot router: one NAS, three hotspot zones, no
// PPPoE at all.
func pilotNAS() nasInfo {
	return nasInfo{
		ID:     "nas-1",
		Vendor: "mikrotik",
		Services: []vendor.ServiceInstance{
			{ID: "svc-warith", Service: "hotspot", ROSServerName: "Warith-Wifi"},
			{ID: "svc-free", Service: "hotspot", ROSServerName: "Warith-Free"},
			{ID: "svc-students", Service: "hotspot", ROSServerName: "Students-Wifi"},
		},
	}
}

func TestResolveService_NoServiceTypeMatchesByStationName(t *testing.T) {
	svc, id := pilotNAS().resolveService(Record{
		ServiceType:     "", // the MikroTik sent none on the accounting packet
		CalledStationID: "Students-Wifi",
	})
	if svc != "hotspot" {
		t.Errorf("service = %q, want hotspot — this is the PPPoE-next-to-a-hotspot-user bug", svc)
	}
	if id != "svc-students" {
		t.Errorf("instance = %q, want svc-students", id)
	}
}

// Even with nothing to disambiguate WHICH zone, the KIND is not in doubt on a
// hotspot-only NAS. Recording it unattributed beats guessing pppoe.
func TestResolveService_NoServiceTypeNoStationFallsBackToTheSoleKind(t *testing.T) {
	svc, id := pilotNAS().resolveService(Record{})
	if svc != "hotspot" {
		t.Errorf("service = %q, want hotspot: every enabled instance on this NAS is a hotspot", svc)
	}
	if id != "" {
		t.Errorf("instance = %q, want unattributed — no attribute identified a zone", id)
	}
}

// A record must never be dropped or mislabelled into a kind the NAS cannot run.
func TestResolveService_MixedNASWithNoHintStaysOnTheHint(t *testing.T) {
	n := nasInfo{ID: "nas-1", Vendor: "mikrotik", Services: []vendor.ServiceInstance{
		{ID: "svc-ppp", Service: "pppoe"},
		{ID: "svc-lobby", Service: "hotspot", ROSServerName: "lobby"},
		{ID: "svc-cafe", Service: "hotspot", ROSServerName: "cafe"},
	}}
	// Nothing identifies the instance and the kinds are mixed: the explicit hint
	// is all there is, and it must be honoured.
	svc, id := n.resolveService(Record{ServiceType: "hotspot"})
	if svc != "hotspot" || id != "" {
		t.Errorf("got service=%q instance=%q, want hotspot/unattributed", svc, id)
	}
	// With no hint at all, pppoe is the documented last resort.
	svc, _ = n.resolveService(Record{})
	if svc != "pppoe" {
		t.Errorf("service = %q, want the pppoe last resort", svc)
	}
}

// An explicit hint still wins its own kind's match (the pre-existing path).
func TestResolveService_ExplicitHintResolvesInstance(t *testing.T) {
	svc, id := pilotNAS().resolveService(Record{ServiceType: "hotspot", CalledStationID: "Warith-Free"})
	if svc != "hotspot" || id != "svc-free" {
		t.Errorf("got service=%q instance=%q, want hotspot/svc-free", svc, id)
	}
}

// A NAS with no registered instances (orphan tolerance) keeps recording.
func TestResolveService_UnregisteredNASStillRecords(t *testing.T) {
	svc, id := nasInfo{ID: zeroUUID}.resolveService(Record{CalledStationID: "lobby"})
	if svc != "pppoe" || id != "" {
		t.Errorf("got service=%q instance=%q, want the pppoe/unattributed sentinel", svc, id)
	}
}

// coarseService must report "unknown" as "", not launder it into pppoe — the
// whole fix hangs off that distinction.
func TestCoarseServiceDoesNotInventPPPoE(t *testing.T) {
	if got := (Record{}).coarseService(); got != "" {
		t.Errorf("a record with no Service-Type reported %q; it must report \"\" (unknown)", got)
	}
	if got := (Record{ServiceType: "garbage"}).coarseService(); got != "" {
		t.Errorf("an unrecognised Service-Type reported %q; it must report \"\" (unknown)", got)
	}
	if got := (Record{ServiceType: "hotspot"}).coarseService(); got != "hotspot" {
		t.Errorf("coarseService = %q, want hotspot", got)
	}
	if got := (Record{ServiceType: "pppoe"}).coarseService(); got != "pppoe" {
		t.Errorf("coarseService = %q, want pppoe", got)
	}
}
