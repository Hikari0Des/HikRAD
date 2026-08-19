package radius

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/radius/vendor"
)

// fakeCoA records Enqueue calls (v4-8: the worker no longer dispatches, it
// only enqueues) and categorizes them the same way the old direct-dispatch
// harness did, so most existing assertions ("N disconnects", "a rate of X")
// still read naturally even though a single combined pool+rate enqueue now
// lands in both `pools` and `rates`.
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
	// enqueueErr, when set, makes every Enqueue call fail (simulates the DB
	// being unreachable — the only way enqueueing itself can fail).
	enqueueErr error
}

func (f *fakeCoA) Enqueue(_ context.Context, ref SessionRef, action CoAAction, attrs []vendor.Attr, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enqueueErr != nil {
		return "", f.enqueueErr
	}
	if action == CoAActionDisconnect {
		f.disconnects = append(f.disconnects, ref)
		return "outbox-1", nil
	}
	for _, a := range attrs {
		switch a.Intent {
		case vendor.IntentRateLimit:
			f.rates = append(f.rates, struct {
				ref  SessionRef
				rate string
			}{ref, a.Value})
		case vendor.IntentAddressPool:
			f.pools = append(f.pools, struct {
				ref  SessionRef
				pool string
			}{ref, a.Value})
		}
	}
	return "outbox-1", nil
}

type testWorker struct {
	*worker
	coa       *fakeCoA
	sessions  []SessionRef
	behavior  behaviorView
	found     bool
	claimed   map[string]bool // dedup keys already claimed
	records   []enforceRecord
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
		enqueue: tw.coa.Enqueue,
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
		record: func(_ context.Context, rec enforceRecord) { tw.records = append(tw.records, rec) },
		now:    time.Now,
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
		name       string
		kind       enforceKind
		b          behaviorView
		wantAction CoAAction
		wantIntent string // "" = no attrs expected (disconnect)
		wantValue  string
		label      string
	}{
		{"quota block", kindQuota, behaviorView{QuotaBehavior: "block"}, CoAActionDisconnect, "", "", "block"},
		{"quota throttle", kindQuota, behaviorView{QuotaBehavior: "throttle", ThrottleRate: "2M/2M"}, CoAActionUpdate, vendor.IntentRateLimit, "2M/2M", "throttle"},
		{"quota throttle no rate", kindQuota, behaviorView{QuotaBehavior: "throttle"}, CoAActionDisconnect, "", "", "throttle"},
		{"quota expired_pool", kindQuota, behaviorView{QuotaBehavior: "expired_pool", ExpiredPoolName: "walled"}, CoAActionUpdate, vendor.IntentAddressPool, "walled", "expired_pool"},
		{"quota expired_pool no pool", kindQuota, behaviorView{QuotaBehavior: "expired_pool"}, CoAActionDisconnect, "", "", "expired_pool"},
		{"quota unknown", kindQuota, behaviorView{QuotaBehavior: "weird"}, "", "", "", "weird"},
		{"expiry block", kindExpired, behaviorView{ExpiryBehavior: "block"}, CoAActionDisconnect, "", "", "block"},
		{"expiry expired_pool", kindExpired, behaviorView{ExpiryBehavior: "expired_pool", ExpiredPoolName: "walled"}, CoAActionUpdate, vendor.IntentAddressPool, "walled", "expired_pool"},
		{"expiry expired_pool no pool", kindExpired, behaviorView{ExpiryBehavior: "expired_pool"}, CoAActionDisconnect, "", "", "block"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			action, attrs, label := planSteps(c.kind, c.b)
			if label != c.label {
				t.Errorf("label = %q, want %q", label, c.label)
			}
			if action != c.wantAction {
				t.Fatalf("action = %q, want %q", action, c.wantAction)
			}
			if c.wantIntent == "" {
				return
			}
			found := false
			for _, a := range attrs {
				if a.Intent == c.wantIntent && a.Value == c.wantValue {
					found = true
				}
			}
			if !found {
				t.Errorf("attrs %+v missing {%s: %s}", attrs, c.wantIntent, c.wantValue)
			}
		})
	}
}

// TestPlanSteps_ExpiredPoolCombinesPoolAndRate proves v4-8's collapse of the
// old sequential move_pool→apply_rate steps into ONE combined attribute set
// (the dispatcher's hybrid fallback then treats the whole thing atomically).
func TestPlanSteps_ExpiredPoolCombinesPoolAndRate(t *testing.T) {
	action, attrs, label := planSteps(kindExpired, behaviorView{ExpiryBehavior: "expired_pool", ExpiredPoolName: "walled", ThrottleRate: "512k/512k"})
	if action != CoAActionUpdate || label != "expired_pool" {
		t.Fatalf("action=%q label=%q, want update/expired_pool", action, label)
	}
	if len(attrs) != 2 {
		t.Fatalf("expected 2 combined attrs, got %+v", attrs)
	}
}

func TestWorker_MultiSessionEnforced(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(3)
	tw.behavior = behaviorView{QuotaBehavior: "throttle", ThrottleRate: "1M/1M"}
	tw.handle(context.Background(), kindQuota, "sub1")

	if len(tw.coa.rates) != 3 {
		t.Fatalf("expected 3 enqueued apply_rate calls, got %d", len(tw.coa.rates))
	}
	for _, r := range tw.coa.rates {
		if r.rate != "1M/1M" {
			t.Errorf("rate = %q, want 1M/1M", r.rate)
		}
	}
	rec := tw.records[len(tw.records)-1]
	if rec.Outcome != "enqueued" || rec.Applied != 3 || rec.Failures != 0 {
		t.Errorf("record = %+v, want enqueued 3/0", rec)
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

// TestWorker_EnqueueFailure_CountsFailure: the ONLY way this worker's own
// enqueue can fail is the DB being unreachable — a NAK/timeout is entirely
// coa_dispatcher.go's concern now (see TestDecideNack_* there).
func TestWorker_EnqueueFailure_CountsFailure(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.behavior = behaviorView{QuotaBehavior: "throttle", ThrottleRate: "1M/1M"}
	tw.coa.enqueueErr = errors.New("db unreachable")
	tw.handle(context.Background(), kindQuota, "sub1")

	rec := tw.records[len(tw.records)-1]
	if rec.Failures != 1 || rec.Outcome != "enqueue_failed" {
		t.Errorf("record = %+v, want 1 failure/enqueue_failed", rec)
	}
}

func TestWorker_ExpiryExpiredPool_CombinedMovePoolAndRate(t *testing.T) {
	tw := newTestWorker(t)
	tw.sessions = sessions(1)
	tw.behavior = behaviorView{ExpiryBehavior: "expired_pool", ExpiredPoolName: "walled", ThrottleRate: "512k/512k"}
	tw.handle(context.Background(), kindExpired, "sub1")

	if len(tw.coa.pools) != 1 || tw.coa.pools[0].pool != "walled" {
		t.Fatalf("expected enqueued move to walled, got %+v", tw.coa.pools)
	}
	if len(tw.coa.rates) != 1 || tw.coa.rates[0].rate != "512k/512k" {
		t.Fatalf("expected enqueued rate 512k/512k alongside it, got %+v", tw.coa.rates)
	}
	// Same session, same row (v4-8 combines pool+rate into one enqueue) — not
	// two independently-retried operations.
	if tw.coa.pools[0].ref.AcctSessionID != tw.coa.rates[0].ref.AcctSessionID {
		t.Fatalf("pool and rate enqueues target different sessions: %+v / %+v", tw.coa.pools[0], tw.coa.rates[0])
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
