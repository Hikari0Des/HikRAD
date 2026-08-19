package radius

// Enqueue is the ONLY way to send a CoA or Disconnect (FR-150 binding rule,
// gate item 1) — a direct dispatch call outside coa_dispatcher.go is a review
// failure. Every caller that used to invoke the old package-level
// Disconnect/ApplyRate/MovePool (the panel's disconnect button, the
// enforcement/limits workers, and billing's renewal/refund CoA restore) now
// calls one of these instead: a row lands in coa_outbox and the dispatcher's
// worker tick takes it from there.

import (
	"context"
	"fmt"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/radius/vendor"
)

// Enqueue inserts one coa_outbox row for ref. attrs is ignored for
// ActionDisconnect. subscriberID may be "" (not every CoA target is
// resolvable to a subscriber, e.g. a stale live-state row). The requesting
// manager, if any, is read from ctx exactly like auth.Audit does — callers
// never pass it explicitly.
func Enqueue(ctx context.Context, ref SessionRef, action CoAAction, attrs []vendor.Attr, reason, subscriberID string) (string, error) {
	c := currentCoA()
	if c == nil || c.db == nil {
		return "", fmt.Errorf("coa: outbox not configured")
	}
	requestedBy := ""
	if m, ok := auth.ManagerFrom(ctx); ok {
		requestedBy = m.ID
	}
	if action == CoAActionDisconnect {
		attrs = nil
	}
	id, err := insertOutbox(ctx, c.db, outboxInsert{
		SubscriberID: subscriberID, NASID: ref.NASID, AcctSessionID: ref.AcctSessionID,
		Username: ref.Username, FramedIP: ref.FramedIP, Service: ref.service(),
		Action: action, Attributes: attrs, Reason: reason, RequestedBy: requestedBy,
	})
	if err != nil {
		return "", err
	}
	_ = auth.Audit(ctx, "coa.enqueue", "session", ref.AcctSessionID, nil, map[string]any{
		"outbox_id": id, "nas_id": ref.NASID, "username": ref.Username,
		"action": string(action), "reason": reason,
	})
	return id, nil
}

// EnqueueDisconnect enqueues a Disconnect-Request (ends the session outright).
func EnqueueDisconnect(ctx context.Context, ref SessionRef, reason, subscriberID string) (string, error) {
	return Enqueue(ctx, ref, CoAActionDisconnect, nil, reason, subscriberID)
}

// EnqueueApplyRate enqueues a rate-limit CoA-Update.
func EnqueueApplyRate(ctx context.Context, ref SessionRef, rate, reason, subscriberID string) (string, error) {
	return Enqueue(ctx, ref, CoAActionUpdate, []vendor.Attr{{Intent: vendor.IntentRateLimit, Value: rate}}, reason, subscriberID)
}

// EnqueueMovePool enqueues an address-pool CoA-Update.
func EnqueueMovePool(ctx context.Context, ref SessionRef, pool, reason, subscriberID string) (string, error) {
	return Enqueue(ctx, ref, CoAActionUpdate, []vendor.Attr{{Intent: vendor.IntentAddressPool, Value: pool}}, reason, subscriberID)
}

// EnqueueUpdate enqueues a CoA-Update carrying an arbitrary attribute set —
// e.g. a combined pool-move + rate-clamp in ONE packet (the expired-pool
// crossing), which the hybrid dispatcher then falls back on as a single unit
// rather than two separately-retried operations.
func EnqueueUpdate(ctx context.Context, ref SessionRef, attrs []vendor.Attr, reason, subscriberID string) (string, error) {
	return Enqueue(ctx, ref, CoAActionUpdate, attrs, reason, subscriberID)
}
