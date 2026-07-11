package monitorsvc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSender records calls and can be made to fail a set number of times.
type fakeSender struct {
	name      string
	failFirst int32 // fail this many attempts, then succeed
	calls     int32
	block     time.Duration // simulate a slow channel
}

func (f *fakeSender) channel() string { return f.name }
func (f *fakeSender) send(ctx context.Context, _ alertMessage) error {
	atomic.AddInt32(&f.calls, 1)
	if f.block > 0 {
		select {
		case <-time.After(f.block):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if n := atomic.LoadInt32(&f.calls); n <= f.failFirst {
		return errors.New("boom")
	}
	return nil
}

// A dead/slow channel must not stop the others from delivering (gate item 6).
func TestDispatcher_FailureIsolation(t *testing.T) {
	// Each channel blocks the same 200 ms so serial dispatch would take ~600 ms
	// while concurrent dispatch takes ~200 ms — an unambiguous isolation check.
	good := &fakeSender{name: "telegram", block: 200 * time.Millisecond}
	dead := &fakeSender{name: "email", failFirst: 99, block: 200 * time.Millisecond}
	inapp := &fakeSender{name: chInApp, block: 200 * time.Millisecond}
	d := newDispatcher(nil, good, dead, inapp)
	d.retries = 0 // isolate concurrency from retry backoff

	start := time.Now()
	res := d.dispatch(context.Background(), []string{"telegram", "email", chInApp}, alertMessage{})
	elapsed := time.Since(start)

	byCh := map[string]delivery{}
	for _, r := range res {
		byCh[r.Channel] = r
	}
	if !byCh["telegram"].OK || !byCh[chInApp].OK {
		t.Fatalf("healthy channels should succeed: %+v", byCh)
	}
	if byCh["email"].OK {
		t.Fatal("dead channel should report failure")
	}
	// Concurrent: total ≈ one channel's time, not the sum — the dead/slow channel
	// didn't block the healthy ones (gate item 6 failure isolation).
	if elapsed > 450*time.Millisecond {
		t.Fatalf("dispatch serialized behind the slow channel (%v)", elapsed)
	}
}

// A transient failure is retried and eventually succeeds.
func TestDispatcher_RetrySucceeds(t *testing.T) {
	flaky := &fakeSender{name: "telegram", failFirst: 1}
	d := newDispatcher(nil, flaky)
	d.retries = 2
	res := d.dispatch(context.Background(), []string{"telegram"}, alertMessage{})
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("expected retry to succeed: %+v", res)
	}
	if flaky.calls < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", flaky.calls)
	}
}

func TestDispatcher_UnknownChannel(t *testing.T) {
	d := newDispatcher(nil, &fakeSender{name: chInApp})
	res := d.dispatch(context.Background(), []string{"sms"}, alertMessage{})
	if len(res) != 1 || res[0].OK {
		t.Fatalf("unknown channel should be a failed delivery: %+v", res)
	}
}

func TestAlertMessage_Text(t *testing.T) {
	if got := (alertMessage{State: "firing", Summary: "x"}).text(); got[:len("🔴")] != "🔴" {
		t.Fatalf("firing prefix wrong: %q", got)
	}
	if got := (alertMessage{State: "resolved", Summary: "x"}).text(); got[:len("🟢")] != "🟢" {
		t.Fatalf("resolved prefix wrong: %q", got)
	}
}
