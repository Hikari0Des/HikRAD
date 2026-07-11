package monitorsvc

// In-app notifications feed (contract C5, FR-36): GET /api/v1/live/notifications
// streams a snapshot of recent alert_events then live deltas as the alert engine
// publishes them on the NotificationsChannel Redis pub/sub. In-app is the one
// channel quiet hours never suppress, so this is the always-on surface (NFR-7).

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

func notificationsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.Error(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	ctx := r.Context()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Snapshot: the most recent notifications so a freshly-opened panel isn't empty.
	snap := recentNotifications(ctx)
	buf, _ := json.Marshal(snap)
	if !writeEvent(w, flusher, "snapshot", buf) {
		return
	}

	if pkgRDB == nil {
		heartbeat(ctx, w, flusher)
		return
	}
	sub := pkgRDB.Subscribe(ctx, NotificationsChannel)
	defer func() { _ = sub.Close() }()
	msgs := sub.Channel()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if !writeComment(w, flusher) {
				return
			}
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			if !writeEvent(w, flusher, "notification", []byte(msg.Payload)) {
				return
			}
		}
	}
}

type notificationView struct {
	Type    string    `json:"type"`
	State   string    `json:"state"`
	Summary string    `json:"summary"`
	At      time.Time `json:"at"`
}

func recentNotifications(ctx context.Context) []notificationView {
	out := []notificationView{}
	if pkgDB == nil {
		return out
	}
	rows, err := pkgDB.Query(ctx,
		`SELECT type, state, summary, at FROM alert_events ORDER BY at DESC LIMIT 20`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n notificationView
		if err := rows.Scan(&n.Type, &n.State, &n.Summary, &n.At); err != nil {
			return out
		}
		n.At = n.At.UTC()
		out = append(out, n)
	}
	return out
}

func heartbeat(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) {
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if !writeComment(w, flusher) {
				return
			}
		}
	}
}

func writeEvent(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) bool {
	out := append([]byte("event: "+event+"\ndata: "), data...)
	out = append(out, '\n', '\n')
	if _, err := w.Write(out); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeComment(w http.ResponseWriter, flusher http.Flusher) bool {
	if _, err := w.Write([]byte(": ping\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
