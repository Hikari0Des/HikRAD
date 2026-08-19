package notify

// Broadcast tab (FR-163): one-off sends to an explicit subscriber set,
// through the exact same queue as everything else (binding rule).

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type broadcastRequest struct {
	SubscriberIDs []string `json:"subscriber_ids"`
	Channel       string   `json:"channel"`
	Subject       string   `json:"subject"`
	Body          string   `json:"body"`
}

func handleBroadcast(w http.ResponseWriter, r *http.Request) {
	var in broadcastRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var ferr []httpapi.FieldError
	if len(in.SubscriberIDs) == 0 {
		ferr = append(ferr, httpapi.FieldError{Field: "subscriber_ids", Message: "at least one subscriber is required"})
	}
	switch Channel(in.Channel) {
	case ChannelTelegram, ChannelEmail, ChannelWhatsApp, ChannelSMS, ChannelInApp:
	default:
		ferr = append(ferr, httpapi.FieldError{Field: "channel", Message: "unknown channel"})
	}
	if in.Body == "" {
		ferr = append(ferr, httpapi.FieldError{Field: "body", Message: "required"})
	}
	if len(ferr) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "invalid broadcast request", ferr...)
		return
	}

	mgr, _ := auth.ManagerFrom(r.Context())
	createdBy := ""
	if mgr != nil {
		createdBy = mgr.ID
	}
	ids, err := EnqueueBroadcast(r.Context(), pkgDB, BroadcastInput{
		SubscriberIDs: in.SubscriberIDs, Channel: Channel(in.Channel), Subject: in.Subject, Body: in.Body, CreatedBy: createdBy,
	})
	if err != nil {
		internalErr(w, "enqueue broadcast", err)
		return
	}
	_ = auth.Audit(r.Context(), "notification.broadcast", "notification_outbox", "", nil, map[string]any{
		"channel": in.Channel, "recipient_count": len(in.SubscriberIDs),
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{"queued": len(ids), "ids": ids})
}
