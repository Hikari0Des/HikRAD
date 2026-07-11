// Package monitorsvc is HikRAD's monitoring backend (Phase 3, Agent 3): the
// ICMP/SNMP probe engine + per-target up/down state machine (FR-34/FR-60), the
// alerts engine with routing/quiet-hours/cooldown across in-app/Telegram/SMTP/
// WhatsApp (FR-36), system self-monitoring (FR-35/FR-40), the dashboard API
// (FR-32) and the per-NAS/per-device status pages.
//
// It is imported by two binaries: hikrad-api mounts the HTTP module (module.go)
// for the read/CRUD APIs, and hikrad-monitor (cmd/hikrad-monitor) runs the
// background loops (runner.go). Both talk to the same Postgres/Redis; the
// process split keeps the API stateless and the probe/alert loops in one place.
//
// It stays strictly read-only against B's `nas` table and A's `settings`, never
// touches internal/{billing,auth,radius} code paths, and owns migrations
// 0230–0239.
package monitorsvc

// Per-target reachability state machine (contract C5, FR-34): N=4 consecutive
// missed ICMP probes flips a target DOWN; the first successful probe flips it
// back UP. Everything here is pure so the flap/recovery matrix is unit-tested
// with no network, DB or Redis.

// downThreshold is the number of consecutive missed ICMP probes that marks a
// target down (contract C5: "N=4 missed ICMP (15 s interval) → event").
const downThreshold = 4

// probeStatus is a target's current reachability.
type probeStatus string

const (
	statusUnknown probeStatus = "unknown" // no probe result yet
	statusUp      probeStatus = "up"
	statusDown    probeStatus = "down"
)

// targetState is the mutable reachability state kept per target between probes.
// Zero value is a fresh target (unknown, no misses). It is owned by a single
// goroutine per target, so it needs no locking.
type targetState struct {
	status     probeStatus
	consecMiss int // consecutive ICMP misses since the last success
}

// transition is what a probe result changed. Down/Up are true only on the edge
// (the probe that crossed the threshold / the first recovery), so callers fire
// exactly one event per outage — never once per continued-down probe.
type transition struct {
	toDown bool
	toUp   bool
}

// observe folds one ICMP probe outcome into the state and reports any edge.
//
//	ok=false: increment misses; cross downThreshold (from not-down) → toDown.
//	ok=true:  reset misses; if we were down (or first-ever success after a
//	          down streak) → toUp. A first-ever success just settles to up.
func (s *targetState) observe(ok bool) transition {
	if ok {
		wasDown := s.status == statusDown
		s.consecMiss = 0
		s.status = statusUp
		return transition{toUp: wasDown}
	}
	s.consecMiss++
	// Fire the down edge exactly once, when the streak first reaches the
	// threshold and we are not already marked down.
	if s.consecMiss >= downThreshold && s.status != statusDown {
		s.status = statusDown
		return transition{toDown: true}
	}
	return transition{}
}
