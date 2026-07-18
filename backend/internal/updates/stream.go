package updates

import (
	"context"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

// streamHandler is GET /api/v1/system/update/stream (C4): re-emits the
// daemon's progress as SSE, either by tailing this process's own in-memory
// relayRun (the common case — this same hikrad-api instance's POST handler
// started it) or, if there is none (a fresh container after a mid-update
// replacement, or simply a page reload with nothing in flight), by polling
// the daemon's status/rollback-status verbs and synthesizing equivalent
// frames (FR-87.2's reconnect fallback).
func streamHandler(w http.ResponseWriter, r *http.Request) {
	if !configured() {
		notConfigured(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.Error(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	currentMu.Lock()
	rr := current
	currentMu.Unlock()

	if rr != nil {
		rr.tail(func(e event) bool { return emitDaemonEvent(w, flusher, e) })
		return
	}

	pollAndStream(r.Context(), w, flusher)
}

func emitDaemonEvent(w http.ResponseWriter, flusher http.Flusher, e event) bool {
	switch e.Type {
	case "stage":
		return writeSSE(w, flusher, "progress", mustJSON(map[string]string{"stage": e.Stage, "ts": e.TS}))
	case "result":
		name := "rolled_back"
		if e.OK != nil && *e.OK {
			name = "done"
		}
		return writeSSE(w, flusher, name, mustJSON(map[string]string{"version": e.Version, "message": e.Message}))
	}
	return true
}

// pollAndStream is the reconnect path: no in-memory run to tail, so it asks
// the daemon directly every couple of seconds. Terminates the stream once a
// completed outcome is found (mirrors the tailed path's own terminal
// event); stays open on heartbeats if nothing has ever run, in case an
// update starts while this connection is attached.
func pollAndStream(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) {
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	lastStage := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if !writeSSEComment(w, flusher) {
				return
			}
		case <-poll.C:
			var st statusResponse
			if err := oneShot("status", &st); err != nil {
				continue
			}
			if st.Stage != lastStage {
				lastStage = st.Stage
				if !writeSSE(w, flusher, "progress", mustJSON(map[string]string{"stage": st.Stage})) {
					return
				}
			}
			if st.Locked {
				continue
			}
			var rb rollbackStatusResponse
			if err := oneShot("rollback-status", &rb); err == nil && rb.LastAction != "" {
				name := "rolled_back"
				if rb.Result == "success" {
					name = "done"
				}
				writeSSE(w, flusher, name, mustJSON(map[string]string{"version": rb.Version, "message": rb.Result}))
				return
			}
			// Genuinely idle, nothing has ever run — keep the connection
			// open on heartbeats rather than closing it.
		}
	}
}
