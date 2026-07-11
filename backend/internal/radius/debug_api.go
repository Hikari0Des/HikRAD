package radius

// RADIUS debug tool backend (FR-39, contract C6). GET /api/v1/live/debug tails
// the capped radius:decisions stream (written by the authorize engine, engine.go)
// over SSE, filtered by username and/or NAS, with the human-readable reason keys
// E localizes ("bad_password", "expired", "session_limit", "unknown_nas", …).
// Permission-gated on nas.view. The stream is capped (decisionStreamMaxLen) so
// retention is bounded; the tail only ever forwards new decisions (XREAD from
// "$") so a long-lived operator view never replays stale history.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// debugEvent is the C6 output shape: {at, username, nas, outcome, reason,
// checks[]}. `nas` is the source IP the decision saw (the debug filter resolves
// a nas_id query param to that IP).
type debugEvent struct {
	At       string   `json:"at"`
	Username string   `json:"username"`
	NAS      string   `json:"nas"`
	Outcome  string   `json:"outcome"`
	Reason   string   `json:"reason"`
	Checks   []string `json:"checks"`
}

func (m *module) liveDebugSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.Error(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	ctx := r.Context()
	username := strings.TrimSpace(r.URL.Query().Get("username"))

	// A nas_id filter resolves to the NAS's source IP, which is what decisions
	// carry. An unknown id yields an IP that matches nothing (empty feed) rather
	// than an error, so the panel can show "no decisions for this NAS yet".
	nasIP := ""
	if nasID := strings.TrimSpace(r.URL.Query().Get("nas_id")); nasID != "" {
		n, err := getNAS(ctx, m.db, nasID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			nasIP = "\x00no-such-nas" // sentinel: never matches a real event
		case err != nil:
			m.internal(w, "resolve nas for debug", err)
			return
		default:
			nasIP = canonicalIP(n.IP)
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	if m.rdb == nil {
		sseDebugHeartbeat(ctx, w, flusher)
		return
	}

	lastID := "$" // only decisions from now on
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := m.rdb.XRead(ctx, &redis.XReadArgs{
			Streams: []string{decisionStream, lastID},
			Block:   15 * time.Second,
			Count:   200,
		}).Result()
		if errors.Is(err, redis.Nil) {
			// Block window elapsed with no new decisions: keep the pipe warm.
			if !sseDebugComment(w, flusher) {
				return
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Transient Redis error: back off briefly, then retry.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for _, stream := range res {
			for _, msg := range stream.Messages {
				lastID = msg.ID
				raw, _ := msg.Values["event"].(string)
				payload, ok := filterDecision(raw, username, nasIP)
				if !ok {
					continue
				}
				if !sseDebugWrite(w, flusher, "decision", payload) {
					return
				}
			}
		}
	}
}

// filterDecision parses a stored decisionEvent, applies the username/NAS
// filters, and renders the C6 output payload. Pure — the filtering + mapping is
// unit-tested without Redis. username matches case-insensitively (usernames are
// the case-insensitive RADIUS identity, FR-1.1); nasIP matches the canonical
// source IP. Empty filters match everything.
func filterDecision(raw, username, nasIP string) ([]byte, bool) {
	if raw == "" {
		return nil, false
	}
	var ev decisionEvent
	if json.Unmarshal([]byte(raw), &ev) != nil {
		return nil, false
	}
	if username != "" && !strings.EqualFold(ev.Username, username) {
		return nil, false
	}
	if nasIP != "" && canonicalIP(ev.NASIP) != nasIP {
		return nil, false
	}
	out := debugEvent{
		At: ev.At, Username: ev.Username, NAS: ev.NASIP,
		Outcome: ev.Outcome, Reason: ev.Reason, Checks: ev.Checks,
	}
	if out.Checks == nil {
		out.Checks = []string{}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return b, true
}

// --- SSE helpers (radius-local; the live package has its own copies) --------

func sseDebugWrite(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) bool {
	if _, err := w.Write([]byte("event: " + event + "\ndata: ")); err != nil {
		return false
	}
	if _, err := w.Write(data); err != nil {
		return false
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func sseDebugComment(w http.ResponseWriter, flusher http.Flusher) bool {
	if _, err := w.Write([]byte(": ping\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func sseDebugHeartbeat(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) {
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if !sseDebugComment(w, flusher) {
				return
			}
		}
	}
}
