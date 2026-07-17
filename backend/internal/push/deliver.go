package push

// Cross-agent seam: Subscribe/Unsubscribe/DeliverToSubscriber are exported for
// D's portalapi to call once its subscriber-token middleware exists (see the
// package doc comment). Delivery helpers here also back monitorsvc's panel
// push channel.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// Subscribe upserts a push subscription for surface+ownerID (manager_id for
// panel, subscriber_id for portal — the two are mutually exclusive per the
// push_subscriptions CHECK constraint).
func Subscribe(ctx context.Context, surface, ownerID, endpoint string, keys Keys) error {
	if surface != surfacePanel && surface != surfacePortal {
		return fmt.Errorf("push: invalid surface %q", surface)
	}
	if ownerID == "" || endpoint == "" || keys.P256dh == "" || keys.Auth == "" {
		return fmt.Errorf("push: incomplete subscription")
	}
	return upsert(ctx, pkgDB, surface, ownerID, endpoint, keys)
}

// Unsubscribe removes a subscription, scoped to its own surface+owner.
func Unsubscribe(ctx context.Context, surface, ownerID, endpoint string) error {
	return remove(ctx, pkgDB, surface, ownerID, endpoint)
}

// DeliverPanel fans payload out to every panel-surface subscription (the
// alert engine's push channel). No subscriptions is a no-op success, not a
// failure (NFR-7 — nothing has installed the panel PWA yet); an error is
// returned only when every existing subscription failed to send, so the
// alert dispatcher's per-channel retry/isolation behaves sensibly.
func DeliverPanel(ctx context.Context, payload Payload) error {
	subs, err := listBySurface(ctx, pkgDB, surfacePanel)
	if err != nil {
		return err
	}
	return deliverAll(ctx, subs, payload)
}

// DeliverToSubscriber sends payload to one subscriber's own portal
// subscriptions (per-subscriber expiry reminder targeting, task 4/4b).
func DeliverToSubscriber(ctx context.Context, subscriberID string, payload Payload) error {
	subs, err := listForSubscriber(ctx, pkgDB, subscriberID)
	if err != nil {
		return err
	}
	return deliverAll(ctx, subs, payload)
}

// DeliverToManager sends payload to one manager's own panel subscriptions
// (v2-2, FR-80.2: a payment ticket landing in/being decided in THEIR queue —
// unlike DeliverPanel's broadcast to every admin's alert subscriptions).
func DeliverToManager(ctx context.Context, managerID string, payload Payload) error {
	subs, err := listForManager(ctx, pkgDB, managerID)
	if err != nil {
		return err
	}
	return deliverAll(ctx, subs, payload)
}

func deliverAll(ctx context.Context, subs []Subscription, payload Payload) error {
	if len(subs) == 0 {
		return nil
	}
	var lastErr error
	okCount := 0
	for _, s := range subs {
		if serr := send(ctx, s, payload); serr != nil {
			if errors.Is(serr, ErrGone) {
				_ = prune(ctx, pkgDB, s.Endpoint)
				continue
			}
			lastErr = serr
			if pkgLog != nil {
				pkgLog.Warn("push: send failed", "endpoint_host", hostOnly(s.Endpoint), "error", serr)
			}
			continue
		}
		okCount++
	}
	if okCount == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}

// hostOnly avoids logging a full push endpoint URL (its path segment is a
// per-subscription bearer token the push service uses to route the message).
func hostOnly(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Host
	}
	return "unknown"
}
