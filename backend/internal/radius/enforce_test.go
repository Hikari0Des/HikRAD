package radius

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// fakeCoA records CoA calls and returns scripted outcomes.
type fakeCoA struct {
	mu          sync.Mutex
	disconnects []SessionRef
	rates       []struct {
		ref  SessionRef
		rate string
	}
	pools []struct {
		ref  SessionRef
		pool string
	}
	// outcome funcs let a test script NAK/timeout per op; default ACK.
	disconnectOut func(SessionRef) CoAResult
	applyRateOut  func(SessionRef, string) CoAResult
	movePoolOut   func(SessionRef, string) CoAResult
}

func (f *fakeCoA) Disconnect(_ context.Context, ref SessionRef) CoAResult {
	f.mu.Lock()
	f.disconnects = append(f.disconnects, ref)
	f.mu.Unlock()
	if f.disconnectOut != nil {
		return f.disconnectOut(ref)
	}
	return CoAResult{Outcome: CoAACK}
}
func (f *fakeCoA) ApplyRate(_ context.Context, ref SessionRef, rate string) CoAResult {
	f.mu.Lock()
	f.rates = append(f.rates, struct {
		ref  SessionRef
		rate string
	}{ref, rate})
	f.mu.Unlock()
	if f.applyRateOut != nil {
		return f.applyRateOut(ref, rate)
	}
	return CoAResult{Outcome: CoAACK}
}
func (f *fakeCoA) MovePool(_ context.Context, ref SessionRef, pool string) CoAResult {
	f.mu.Lock()
	f.pools = append(f.pools, struct {
		ref  SessionRef
		pool string
	}{ref, pool})
	f.mu.Unlock()
	if f.movePoolOut != nil {
		return f.movePoolOut(ref, pool)
	}
	return CoAResult{Outcome: CoAACK}
}

type testWorker struct {
	*worker
	coa       *fakeCoA
	sessions  []SessionRef
	behavior  behaviorView
	found     bool
	claimed   map[string]bool // dedup keys already claimed
	records   []enforceRecord
	failures  int
	claimErr  error
	listErr   error
	behaveErr error
}

func newTestWorker(t *testing.T) *testWorker {
	t.Helper()
	tw := &testWorker{
		coa:     &fakeCoA{},
		found:   true,
		claimed: map[string]bool{},
	}
	tw.worker = &worker{
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		listSessions: func(_ context.Context, _ string) ([]SessionRef, error) { return tw.sessions, tw.listErr },
		behavior: func(_ context.Context, _ string) (behaviorView, bool, error) {
			return tw.behavior, tw.found, tw.behaveErr
		},
		disconnect: tw.coa.Disconnect,
		applyRate:  tw.coa.ApplyRate,
		movePool:   tw.coa.MovePool,
		claim: func(_ context.Context, _ enforceKind, _, key string) (bool, error) {
			if tw.claimErr != nil {
				return false, tw.claimErr
			}
			if tw.claimed[key] {
				return false, nil
			}
			tw.claimed[key] = true
			return true, nil
		},
		record:      func(_ context.Context, rec enforceRecord) { tw.records = append(tw.records, rec) },
		incFailures: func(_ context.Context, n int) { tw.failures += n },
		sleep:       func(time.Duration) {},
		now:         time.Now,
		maxAttempts: 3,
	}
	return tw
}

func sessions(n int) []SessionRef {
	out := make([]SessionRef, n)
	for i := range out {
		out[i] = SessionRef{NASID: "nas1", AcctSessionID: "sess" + itoa(i), Username: "u"}
	}
	return out
}

func TestPlanSteps_Matrix(t *testing.T) {
	cases := []struct {
		name     string
		kind     enforceKind
		b        behaviorView
		wantOps  []string
		wantFall bool // first step fallback
		label    string
	}{
		{"quota block", kindQuota, behaviorView{QuotaBehavior: "block"}, []string{"disconnect"}, false, "block"},
		{"quota throttle", kindQuota, behaviorView{QuotaBehavior: "throttle", ThrottleRate: "2M/2M"}, []string{"apply_rate"}, true, "throttle"},
		{"quota throttle no rate", kindQuota, behaviorView{QuotaBehavior: "throttle"}, []string{"disconnect"}, false, "throttle"},
		{"quota expired_pool", kindQuota, behaviorView{QuotaBehavior: "expired_pool", ExpiredPoolName: "walled"}, []string{"move_pool"}, true, "expired_pool"},
		{"quota expired_pool no pool", kindQuota, behaviorView{QuotaBehavior: "expired_pool"}, []string{"disconnect"}, false, "expired_pool"},
		{"quota unknown", kindQuota, behaviorView{QuotaBehavior: "weird"}, nil, false, "weird"},
		{"expiry block", kindExpired, behaviorView{ExpiryBehavior: "block"}, []string{"disconnect"}, false, "block"},
		{"expiry expired_pool", kindExpired, behaviorView{ExpiryBehavior: "expired_pool", ExpiredPoolName: "walled"}, []string{"move_pool", "apply_rate"}, true, "expired_pool"},
		{"expiry expired_pool no pool", kindExpired, behaviorView{ExpiryBehavior: "expired_pool"}, []string{"disconnect"}, false, "block"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steps, label := planSteps(c.kind, c.b)
			if label != c.label {
				t.Errorf("label = %q, want %q", label, c.label)
			}
			if len(steps) != len(c.wantOps) {
				t.Fatalf("got %d steps %+v, want ops %v", len(steps), steps, c.wantOps)
			}
			for i, op := range c.wantOps {
				if steps[i].op != op {
					t.Errorf("step %d op = %q, want %q", i, steps[i].op, op)
				}
			}
			if len(steps) > 0 && steps[0].fallback != c.wantFall {
				t.Errorf("step0 fallback = %v, want %v", steps[0].fallback, c.wantFall)
			}
		})
	}
}

func TestWorker_MultiSessionEnforced(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(3)
	tw.behavior = behaviorView{QuotaBehavior: "throttle", ThrottleRate: "1M/1M"}
	tw.handle(context.Background(), kindQuota, "sub1")

	if len(tw.coa.rates) != 3 {
		t.Fatalf("expected 3 ApplyRate calls, got %d", len(tw.coa.rates))
	}
	for _, r := range tw.coa.rates {
		if r.rate != "1M/1M" {
			t.Errorf("rate = %q, want 1M/1M", r.rate)
		}
	}
	rec := tw.records[len(tw.records)-1]
	if rec.Outcome != "applied" || rec.Applied != 3 || rec.Failures != 0 {
		t.Errorf("record = %+v, want applied 3/0", rec)
	}
}

func TestWorker_Idempotent(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.behavior = behaviorView{QuotaBehavior: "block"}
	tw.handle(context.Background(), kindQuota, "sub1")
	tw.handle(context.Background(), kindQuota, "sub1") // re-delivered same cycle

	if len(tw.coa.disconnects) != 1 {
		t.Fatalf("expected 1 disconnect (idempotent), got %d", len(tw.coa.disconnects))
	}
}

func TestWorker_ThrottleNAKFallsBackToDisconnect(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.behavior = behaviorView{QuotaBehavior: "throttle", ThrottleRate: "1M/1M"}
	tw.coa.applyRateOut = func(SessionRef, string) CoAResult { return CoAResult{Outcome: CoANAK} }
	tw.handle(context.Background(), kindQuota, "sub1")

	if len(tw.coa.rates) != 1 {
		t.Fatalf("expected 1 ApplyRate attempt, got %d", len(tw.coa.rates))
	}
	if len(tw.coa.disconnects) != 1 {
		t.Fatalf("expected fallback Disconnect, got %d", len(tw.coa.disconnects))
	}
	rec := tw.records[len(tw.records)-1]
	if rec.Applied != 1 || rec.Failures != 0 {
		t.Errorf("record = %+v, want applied 1/0 (fallback ACKed)", rec)
	}
	if rec.Detail[0].Path != "apply_rate→disconnect" {
		t.Errorf("path = %q, want apply_rate→disconnect", rec.Detail[0].Path)
	}
}

func TestWorker_ThrottleNAKAndFallbackFail_CountsFailure(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.behavior = behaviorView{QuotaBehavior: "throttle", ThrottleRate: "1M/1M"}
	tw.coa.applyRateOut = func(SessionRef, string) CoAResult { return CoAResult{Outcome: CoANAK} }
	tw.coa.disconnectOut = func(SessionRef) CoAResult { return CoAResult{Outcome: CoATimeout} }
	tw.handle(context.Background(), kindQuota, "sub1")

	rec := tw.records[len(tw.records)-1]
	if rec.Failures != 1 || rec.Outcome != "failed" {
		t.Errorf("record = %+v, want 1 failure/failed", rec)
	}
	if tw.failures != 1 {
		t.Errorf("enforcement_failures inc = %d, want 1", tw.failures)
	}
	// Retried maxAttempts times: 3 apply + 3 disconnect fallback.
	if len(tw.coa.rates) != 3 {
		t.Errorf("expected 3 retried ApplyRate, got %d", len(tw.coa.rates))
	}
}

func TestWorker_ExpiryExpiredPool_MovePoolThenRate(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.behavior = behaviorView{ExpiryBehavior: "expired_pool", ExpiredPoolName: "walled", ThrottleRate: "512k/512k"}
	tw.handle(context.Background(), kindExpired, "sub1")

	if len(tw.coa.pools) != 1 || tw.coa.pools[0].pool != "walled" {
		t.Fatalf("expected MovePool(walled), got %+v", tw.coa.pools)
	}
	if len(tw.coa.rates) != 1 || tw.coa.rates[0].rate != "512k/512k" {
		t.Fatalf("expected ApplyRate(512k/512k) after move, got %+v", tw.coa.rates)
	}
}

func TestWorker_ExpiryExpiredPool_MoveNAKFallsBack(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.behavior = behaviorView{ExpiryBehavior: "expired_pool", ExpiredPoolName: "walled"}
	tw.coa.movePoolOut = func(SessionRef, string) CoAResult { return CoAResult{Outcome: CoANAK} }
	tw.handle(context.Background(), kindExpired, "sub1")

	if len(tw.coa.disconnects) != 1 {
		t.Fatalf("expected fallback disconnect, got %d", len(tw.coa.disconnects))
	}
	// After a fallback-disconnect the rate step must NOT run (session gone).
	if len(tw.coa.rates) != 0 {
		t.Errorf("expected no ApplyRate after fallback disconnect, got %d", len(tw.coa.rates))
	}
}

func TestWorker_OfflineSubscriber_NoOp(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = nil
	tw.handle(context.Background(), kindExpired, "sub1")
	if len(tw.coa.disconnects)+len(tw.coa.rates)+len(tw.coa.pools) != 0 {
		t.Errorf("expected no CoA for offline subscriber")
	}
	if tw.records[0].Outcome != "no_sessions" {
		t.Errorf("outcome = %q, want no_sessions", tw.records[0].Outcome)
	}
}

func TestWorker_UnknownSubscriber_NotFound(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.found = false
	tw.handle(context.Background(), kindQuota, "sub1")
	if tw.records[0].Outcome != "not_found" {
		t.Errorf("outcome = %q, want not_found", tw.records[0].Outcome)
	}
}

func TestWorker_EmptySubscriberID_NoOp(t *testing.T) {
	tw := newTestWorker(t)
	tw.handle(context.Background(), kindQuota, "")
	if len(tw.records) != 0 {
		t.Errorf("expected no records for empty subscriber id")
	}
}
