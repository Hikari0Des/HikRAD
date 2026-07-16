package live

// The live package's whole job for other modules is the seams it installs on
// radius. A seam that is DEFINED but never wired is invisible: radius falls back
// to "0" by design (so the panel renders before C boots), so an unwired counter
// looks exactly like "nobody is online" forever.
//
// That is not hypothetical — radius.SetServiceLiveCounts shipped in v2 phase 1
// with no caller, and the NAS page reported 0 live users per service from the
// day it was written until an operator noticed (docs/ops/known-issues.md).
// These tests assert the wiring exists, because nothing else does.

import (
	"log/slog"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
)

func TestRegisterWiresEveryRadiusSeam(t *testing.T) {
	// Register with nil handles: the seams must be installed regardless, and each
	// degrades to zero/empty rather than panicking on a nil Redis.
	m := &Module{}
	m.Register(chi.NewRouter(), httpapi.Deps{Log: slog.Default()})

	t.Run("service live counts", func(t *testing.T) {
		// The bug: this returned 0 for every instance because nothing called
		// SetServiceLiveCounts. A wired-but-empty seam returns a usable map (or
		// nil from a nil Redis) without panicking; an UNWIRED one is what we are
		// guarding against, and radius exposes that through the getter below.
		if !radius.ServiceLiveCountsWired() {
			t.Fatal("radius.SetServiceLiveCounts was never called by live.Register — the NAS services sub-list will report 0 online forever")
		}
	})

	t.Run("subscriber + nas counters do not panic on nil redis", func(t *testing.T) {
		if got := Count("sub-1", "hotspot"); got != 0 {
			t.Errorf("Count with no Redis = %d, want 0", got)
		}
		if got := NASCount("nas-1"); got != 0 {
			t.Errorf("NASCount with no Redis = %d, want 0", got)
		}
	})
}

// ServiceCounts buckets live sessions by their resolved instance. A session that
// never resolved to one is recorded but unattributable, and must not be counted
// against some arbitrary zone.
func TestServiceCountsIgnoresUnattributedSessions(t *testing.T) {
	if got := ServiceCounts(); len(got) != 0 {
		t.Fatalf("ServiceCounts with no Redis = %+v, want empty", got)
	}
}
