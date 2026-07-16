package radius

// Time-of-day sweeps (FR-11, contract C4). Profiles may define windows that
// grant a speed boost and/or a quota exemption (e.g. free night speed 00:00–06:00
// Asia/Baghdad). At each window boundary this engine:
//   - publishes tod.window {profile_id, active} on Redis so C can flip
//     usage_points.exempt for the window (usage exemption marking), and
//   - CoA-ApplyRate's the boosted rate (entering) or the normal rate (leaving)
//     to every live session on the affected profile.
// Auth-time attributes for the window are already correct via D's AuthView (the
// engine emits the right rate on the next Access-Request); the sweep only exists
// so *already-online* sessions change at the boundary without redialing.
//
// D owns the TOD schema (profiles, migration 0200s); B reads it through the
// injected TODProvider seam (same avoid-import-cycle pattern as PolicyProvider).
// Until D wires it, the engine is a safe no-op.

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/redis/go-redis/v9"
)

// chanTODWindow is the frozen channel C subscribes to for exemption marking.
const chanTODWindow = "tod.window"

// todSweepInterval is how often boundaries are checked; a minute is fine for
// minute-granularity windows and cheap (one provider read + a set diff).
const todSweepInterval = time.Minute

// TODWindow is one profile's time-of-day rule (contract C4 extension this phase).
// Start/End are minutes since local midnight (Asia/Baghdad); a window with
// End < Start wraps past midnight. BoostRate/NormalRate are abstract "rx/tx"
// intents ("" = no speed change, exemption-only window).
type TODWindow struct {
	ProfileID  string
	Label      string
	StartMin   int
	EndMin     int
	BoostRate  string
	NormalRate string
	Exempt     bool
}

// TODProvider is D's read-model for TOD rules and subscriber→profile resolution.
type TODProvider interface {
	// TODWindows returns every configured window across all profiles.
	TODWindows(ctx context.Context) ([]TODWindow, error)
	// ProfilesForSubscribers maps subscriber id → profile id for the given ids
	// (so the sweep can target a profile's live sessions).
	ProfilesForSubscribers(ctx context.Context, subIDs []string) (map[string]string, error)
}

var (
	todMu       sync.RWMutex
	todProvider TODProvider
)

// SetTODProvider installs D's TOD read-model. Called once at boot from D.
func SetTODProvider(p TODProvider) {
	todMu.Lock()
	todProvider = p
	todMu.Unlock()
}

func currentTODProvider() TODProvider {
	todMu.RLock()
	defer todMu.RUnlock()
	return todProvider
}

// windowActive reports whether minuteOfDay falls inside w. Handles the
// midnight-wrapping case (End < Start). The window is half-open [Start, End).
func windowActive(w TODWindow, minuteOfDay int) bool {
	if w.StartMin == w.EndMin {
		return false // zero-length window is never active
	}
	if w.StartMin < w.EndMin {
		return minuteOfDay >= w.StartMin && minuteOfDay < w.EndMin
	}
	// Wraps midnight, e.g. 23:00 (1380) .. 07:00 (420).
	return minuteOfDay >= w.StartMin || minuteOfDay < w.EndMin
}

func windowKey(w TODWindow) string {
	return w.ProfileID + ":" + itoa(w.StartMin) + "-" + itoa(w.EndMin)
}

// todEngine holds the transition state and seams. Every dependency is injected
// so boundary math + dispatch are unit-testable with no Redis/DB/NAS/clock.
type todEngine struct {
	log        *slog.Logger
	windows    func(ctx context.Context) ([]TODWindow, error)
	profilesOf func(ctx context.Context, subIDs []string) (map[string]string, error)
	sessions   func(ctx context.Context) ([]livestate.State, error)
	applyRate  func(ctx context.Context, ref SessionRef, rate string) CoAResult
	publish    func(ctx context.Context, profileID string, active bool)
	now        func() time.Time // local time (Asia/Baghdad)

	state map[string]bool // windowKey → last-known active (baseline set on first tick)
}

func newTODEngine(log *slog.Logger) *todEngine {
	return &todEngine{
		log:   log,
		state: map[string]bool{},
	}
}

func minuteOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }

// tick evaluates every window once; on a state transition it publishes tod.window
// and sweeps the profile's live sessions to the boost/normal rate.
func (e *todEngine) tick(ctx context.Context) {
	wins, err := e.windows(ctx)
	if err != nil {
		e.log.Warn("tod: read windows failed", "error", err)
		return
	}
	mod := minuteOfDay(e.now())
	for _, w := range wins {
		key := windowKey(w)
		active := windowActive(w, mod)
		prev, seen := e.state[key]
		e.state[key] = active
		if !seen {
			// First observation is the baseline — never fire a sweep on startup
			// for a window that is already in its steady state.
			continue
		}
		if active == prev {
			continue
		}
		// Boundary crossed.
		e.publish(ctx, w.ProfileID, active)
		rate := w.NormalRate
		if active {
			rate = w.BoostRate
		}
		if rate != "" {
			e.sweepProfile(ctx, w.ProfileID, rate, w.Label, active)
		}
	}
}

// sweepProfile ApplyRate's rate to every live session whose subscriber is on
// profileID. NAK/timeout is logged (a boost/de-boost failing is not
// alert-worthy the way a quota/expiry enforcement failure is — the next auth
// corrects it); it does not fall back to Disconnect.
func (e *todEngine) sweepProfile(ctx context.Context, profileID, rate, label string, active bool) {
	all, err := e.sessions(ctx)
	if err != nil {
		e.log.Warn("tod: list sessions failed", "error", err, "profile", profileID)
		return
	}
	if len(all) == 0 {
		return
	}
	ids := make([]string, 0, len(all))
	seen := map[string]struct{}{}
	for _, s := range all {
		if s.SubscriberID == "" {
			continue
		}
		if _, ok := seen[s.SubscriberID]; ok {
			continue
		}
		seen[s.SubscriberID] = struct{}{}
		ids = append(ids, s.SubscriberID)
	}
	pmap, err := e.profilesOf(ctx, ids)
	if err != nil {
		e.log.Warn("tod: resolve profiles failed", "error", err, "profile", profileID)
		return
	}
	swept := 0
	for _, s := range all {
		if pmap[s.SubscriberID] != profileID {
			continue
		}
		res := e.applyRate(ctx, SessionRef{
			NASID: s.NASID, AcctSessionID: s.AcctSessionID, Username: s.Username, FramedIP: s.IP,
			Service: s.Service,
		}, rate)
		if res.Ok() {
			swept++
		}
	}
	e.log.Info("tod: window sweep", "profile", profileID, "window", label, "active", active, "rate", rate, "sessions_swept", swept)
}

// --- production wiring ------------------------------------------------------

// startTODSweeps runs the boundary check every todSweepInterval for the process
// lifetime. A nil Redis client (unit context) is a no-op.
func (m *module) startTODSweeps(ctx context.Context) {
	if m.rdb == nil {
		return
	}
	loc, err := time.LoadLocation("Asia/Baghdad")
	if err != nil {
		// Fall back to fixed +03:00 (Iraq has no DST) if the tzdata is missing.
		loc = time.FixedZone("Asia/Baghdad", 3*60*60)
	}
	e := newTODEngine(m.log)
	e.windows = func(ctx context.Context) ([]TODWindow, error) {
		p := currentTODProvider()
		if p == nil {
			return nil, nil
		}
		return p.TODWindows(ctx)
	}
	e.profilesOf = func(ctx context.Context, subIDs []string) (map[string]string, error) {
		p := currentTODProvider()
		if p == nil {
			return map[string]string{}, nil
		}
		return p.ProfilesForSubscribers(ctx, subIDs)
	}
	e.sessions = func(ctx context.Context) ([]livestate.State, error) { return livestate.All(ctx, m.rdb) }
	e.applyRate = ApplyRate
	e.publish = func(ctx context.Context, profileID string, active bool) { publishTODWindow(ctx, m.rdb, profileID, active) }
	e.now = func() time.Time { return time.Now().In(loc) }

	m.log.Info("tod sweep engine started", "interval", todSweepInterval.String())
	ticker := time.NewTicker(todSweepInterval)
	defer ticker.Stop()
	e.tick(ctx) // establish baseline immediately
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			e.tick(tctx)
			cancel()
		}
	}
}

func publishTODWindow(ctx context.Context, rdb *redis.Client, profileID string, active bool) {
	if rdb == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{"profile_id": profileID, "active": active})
	if err := rdb.Publish(ctx, chanTODWindow, payload).Err(); err != nil {
		// Best-effort: a missed publish just means C keeps the previous exempt
		// state until the next boundary; not fatal.
		return
	}
}
