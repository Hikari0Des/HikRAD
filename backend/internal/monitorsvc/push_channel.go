package monitorsvc

// pushSender adapts the alert engine's dispatcher to C's own Web Push backend
// (contract C4, FR-54.4) as the panel surface's 4th channel. Delivery
// isolation is inherited for free: the dispatcher already runs every channel
// concurrently with its own retry budget, so a dead/un-subscribed push
// endpoint never delays Telegram/email/WhatsApp (gate item 6).

import (
	"context"

	"github.com/hikrad/hikrad/internal/push"
)

type pushSender struct{}

func (pushSender) channel() string { return chPush }

func (pushSender) send(ctx context.Context, m alertMessage) error {
	return push.DeliverPanel(ctx, push.Payload{
		TitleKey: "push.alert." + m.RuleType + ".title",
		BodyKey:  "push.alert." + m.RuleType + ".body",
		Params:   alertPushParams(m),
		URL:      "/alerts",
	})
}

// alertPushParams carries the rule's structured payload plus fire state —
// never the rendered summary text (edge case: push payloads are keys+params
// only, F's PWA localizes client-side).
func alertPushParams(m alertMessage) map[string]any {
	params := make(map[string]any, len(m.Payload)+1)
	params["state"] = m.State
	for k, v := range m.Payload {
		params[k] = v
	}
	return params
}
