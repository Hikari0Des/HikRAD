package monitorsvc

// Frozen channel names (contract C5 enum). Actual delivery moved to
// internal/notify (v4-10) — alerts.go's Fire enqueues into the one shared
// throttled queue instead of dispatching to these channels inline; this
// file keeps only the name constants rules_api.go's validChannels and
// alerts.go's effectiveChannels still need.
const (
	chInApp    = "inapp"
	chTelegram = "telegram"
	chEmail    = "email"
	chWhatsApp = "whatsapp"
	chPush     = "push"
)

// NotificationsChannel is the Redis pub/sub the in-app SSE feed forwards —
// kept here (unchanged name/value) because monitorsvc's own live-feed
// subscriber (history.go / the SSE handler) still reads it directly;
// internal/notify's inAppSender publishes to the exact same channel name.
const NotificationsChannel = "alerts:notifications"
