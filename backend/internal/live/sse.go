package live

// Server-Sent Events live feed (contract C6, FR-31): GET /api/v1/live/sessions
// emits a `snapshot` of the current sessions, then `upsert`/`remove` deltas as
// the consumer (hikrad-acct, cross-process) publishes them on Redis
// livestate.EventsChannel. Filters and manager scope are applied identically to
// the snapshot and the deltas via matchState.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live/livestate"
)

func (m *Module) liveSessionsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.Error(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	ctx := r.Context()
	f := filterFromQuery(r)
	scope := auth.ScopeFilter(ctx)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering (Caddy/nginx)
	w.WriteHeader(http.StatusOK)

	// Snapshot first (FR-31: table renders immediately, then live-updates).
	snapshot, err := List(ctx, f, scope)
	if err != nil {
		m.log.Warn("live sse: snapshot failed", "error", err)
		snapshot = nil
	}
	if snapshot == nil {
		snapshot = []livestate.State{}
	}
	buf, _ := json.Marshal(snapshot)
	if !writeSSE(w, flusher, "snapshot", buf) {
		return
	}

	if pkgRDB == nil {
		// No Redis: nothing more to stream; hold the connection open with pings.
		m.heartbeatOnly(ctx, w, flusher)
		return
	}

	sub := pkgRDB.Subscribe(ctx, livestate.EventsChannel)
	defer func() { _ = sub.Close() }()
	msgs := sub.Channel()

	// Per-connection attribute cache: profile/owner rarely change over a feed's
	// lifetime, so resolve each subscriber once.
	attrCache := map[string]subjectAttrs{}
	resolve := func(subID string) subjectAttrs {
		if subID == "" {
			return subjectAttrs{}
		}
		if a, ok := attrCache[subID]; ok {
			return a
		}
		a := resolveSubjects(ctx, pkgDB, []string{subID})[subID]
		attrCache[subID] = a
		return a
	}

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if !writeSSEComment(w, flusher) {
				return
			}
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			var evt livestate.Event
			if json.Unmarshal([]byte(msg.Payload), &evt) != nil {
				continue
			}
			switch evt.Op {
			case livestate.OpRemove:
				// Removes carry only the field id; forward so clients drop the
				// row (a no-op for rows they never had).
				if !writeSSE(w, flusher, "remove", []byte(`{"field":`+jsonString(evt.Field)+`}`)) {
					return
				}
			case livestate.OpUpsert:
				if evt.State == nil {
					continue
				}
				if !matchState(*evt.State, f, scope, resolve(evt.State.SubscriberID)) {
					continue
				}
				b, _ := json.Marshal(evt.State)
				if !writeSSE(w, flusher, "upsert", b) {
					return
				}
			}
		}
	}
}

func (m *Module) heartbeatOnly(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) {
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if !writeSSEComment(w, flusher) {
				return
			}
		}
	}
}

// writeSSE emits one named event. Returns false when the write fails (client
// gone) so the caller can stop.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) bool {
	if _, err := w.Write(encodeSSE(event, data)); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeSSEComment(w http.ResponseWriter, flusher http.Flusher) bool {
	if _, err := w.Write([]byte(": ping\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// encodeSSE renders an SSE frame: `event: <name>\ndata: <payload>\n\n`. Data is
// assumed single-line JSON (no embedded newlines), which is always true for the
// compact json.Marshal output used here.
func encodeSSE(event string, data []byte) []byte {
	out := make([]byte, 0, len(event)+len(data)+16)
	out = append(out, "event: "...)
	out = append(out, event...)
	out = append(out, '\n')
	out = append(out, "data: "...)
	out = append(out, data...)
	out = append(out, '\n', '\n')
	return out
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func filterFromQuery(r *http.Request) Filter {
	q := r.URL.Query()
	return Filter{
		NASID:     q.Get("nas_id"),
		ProfileID: q.Get("profile_id"),
		ManagerID: q.Get("manager_id"),
		Q:         q.Get("q"),
		Service:   q.Get("service"),
	}
}
