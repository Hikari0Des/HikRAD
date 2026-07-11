package radius

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
)

func TestWindowActive(t *testing.T) {
	day := TODWindow{StartMin: 9 * 60, EndMin: 17 * 60}    // 09:00–17:00
	night := TODWindow{StartMin: 23 * 60, EndMin: 7 * 60}  // 23:00–07:00 (wraps)
	zero := TODWindow{StartMin: 5 * 60, EndMin: 5 * 60}    // degenerate

	cases := []struct {
		w   TODWindow
		min int
		on  bool
	}{
		{day, 8 * 60, false},
		{day, 9 * 60, true},
		{day, 12 * 60, true},
		{day, 17 * 60, false}, // half-open end
		{night, 23 * 60, true},
		{night, 2 * 60, true},
		{night, 6*60 + 59, true},
		{night, 7 * 60, false},
		{night, 12 * 60, false},
		{zero, 5 * 60, false},
	}
	for _, c := range cases {
		if got := windowActive(c.w, c.min); got != c.on {
			t.Errorf("windowActive(%d-%d, %d) = %v, want %v", c.w.StartMin, c.w.EndMin, c.min, got, c.on)
		}
	}
}

// todFixture wires a todEngine with controllable clock/sessions/CoA capture.
type todFixture struct {
	*todEngine
	minute   int
	wins     []TODWindow
	sessions []livestate.State
	profiles map[string]string
	rates    []struct {
		ref  SessionRef
		rate string
	}
	published []struct {
		profile string
		active  bool
	}
}

func newTODFixture() *todFixture {
	f := &todFixture{profiles: map[string]string{}}
	f.todEngine = &todEngine{
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		state: map[string]bool{},
		windows: func(context.Context) ([]TODWindow, error) { return f.wins, nil },
		profilesOf: func(_ context.Context, ids []string) (map[string]string, error) {
			out := map[string]string{}
			for _, id := range ids {
				if p, ok := f.profiles[id]; ok {
					out[id] = p
				}
			}
			return out, nil
		},
		sessions: func(context.Context) ([]livestate.State, error) { return f.sessions, nil },
		applyRate: func(_ context.Context, ref SessionRef, rate string) CoAResult {
			f.rates = append(f.rates, struct {
				ref  SessionRef
				rate string
			}{ref, rate})
			return CoAResult{Outcome: CoAACK}
		},
		publish: func(_ context.Context, profileID string, active bool) {
			f.published = append(f.published, struct {
				profile string
				active  bool
			}{profileID, active})
		},
	}
	f.todEngine.now = func() time.Time {
		return time.Date(2026, 7, 11, f.minute/60, f.minute%60, 0, 0, time.UTC)
	}
	return f
}

func TestTOD_BaselineNoFireThenTransition(t *testing.T) {
	f := newTODFixture()
	f.wins = []TODWindow{{ProfileID: "p1", Label: "night", StartMin: 0, EndMin: 6 * 60, BoostRate: "50M/50M", NormalRate: "10M/10M", Exempt: true}}
	f.profiles = map[string]string{"sub1": "p1"}
	f.sessions = []livestate.State{{SubscriberID: "sub1", NASID: "nas1", AcctSessionID: "s1", Username: "u1"}}

	// 05:00 — window active. First tick sets baseline, must NOT fire.
	f.minute = 5 * 60
	f.tick(context.Background())
	if len(f.published) != 0 || len(f.rates) != 0 {
		t.Fatalf("baseline tick fired: published=%v rates=%v", f.published, f.rates)
	}

	// 06:00 — window closes. Transition active→inactive: publish + de-boost.
	f.minute = 6 * 60
	f.tick(context.Background())
	if len(f.published) != 1 || f.published[0].active {
		t.Fatalf("expected publish active=false, got %+v", f.published)
	}
	if len(f.rates) != 1 || f.rates[0].rate != "10M/10M" {
		t.Fatalf("expected de-boost to normal 10M/10M, got %+v", f.rates)
	}
}

func TestTOD_EnterWindowBoosts(t *testing.T) {
	f := newTODFixture()
	f.wins = []TODWindow{{ProfileID: "p1", StartMin: 0, EndMin: 6 * 60, BoostRate: "50M/50M", NormalRate: "10M/10M"}}
	f.profiles = map[string]string{"sub1": "p1"}
	f.sessions = []livestate.State{{SubscriberID: "sub1", NASID: "nas1", AcctSessionID: "s1", Username: "u1"}}

	// Baseline outside window.
	f.minute = 12 * 60
	f.tick(context.Background())
	// Enter window at 00:00.
	f.minute = 0
	f.tick(context.Background())

	if len(f.published) != 1 || !f.published[0].active {
		t.Fatalf("expected publish active=true, got %+v", f.published)
	}
	if len(f.rates) != 1 || f.rates[0].rate != "50M/50M" {
		t.Fatalf("expected boost to 50M/50M, got %+v", f.rates)
	}
}

func TestTOD_SweepOnlyAffectsProfileSessions(t *testing.T) {
	f := newTODFixture()
	f.wins = []TODWindow{{ProfileID: "p1", StartMin: 0, EndMin: 6 * 60, BoostRate: "50M/50M", NormalRate: "10M/10M"}}
	f.profiles = map[string]string{"sub1": "p1", "sub2": "p2"}
	f.sessions = []livestate.State{
		{SubscriberID: "sub1", NASID: "nas1", AcctSessionID: "s1", Username: "u1"},
		{SubscriberID: "sub2", NASID: "nas1", AcctSessionID: "s2", Username: "u2"},
	}
	f.minute = 12 * 60
	f.tick(context.Background())
	f.minute = 1 * 60
	f.tick(context.Background())

	if len(f.rates) != 1 || f.rates[0].ref.AcctSessionID != "s1" {
		t.Fatalf("expected only p1's session swept, got %+v", f.rates)
	}
}

func TestTOD_ExemptOnlyWindowPublishesWithoutCoA(t *testing.T) {
	f := newTODFixture()
	// No BoostRate/NormalRate: exemption-only window (quota-free, same speed).
	f.wins = []TODWindow{{ProfileID: "p1", StartMin: 0, EndMin: 6 * 60, Exempt: true}}
	f.profiles = map[string]string{"sub1": "p1"}
	f.sessions = []livestate.State{{SubscriberID: "sub1", NASID: "nas1", AcctSessionID: "s1", Username: "u1"}}

	f.minute = 12 * 60
	f.tick(context.Background())
	f.minute = 0
	f.tick(context.Background())

	if len(f.published) != 1 || !f.published[0].active {
		t.Fatalf("expected tod.window publish, got %+v", f.published)
	}
	if len(f.rates) != 0 {
		t.Fatalf("exempt-only window must not CoA, got %+v", f.rates)
	}
}
