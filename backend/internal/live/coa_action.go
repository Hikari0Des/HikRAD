package live

// Disconnect action (contract C5 consumer / FR-31.3). The panel's Disconnect
// button posts here; this handler resolves the live session, enforces manager
// scope, then ENQUEUES a Disconnect through B's CoA outbox (v4-8, FR-150 —
// enqueue is the only way to send a CoA; a direct dispatch here would be a
// review failure). The response is therefore 202 with the outbox row id, not
// a synchronous ack/nak — coa_dispatcher.go sends it on its own worker tick.
// The live row is removed by the resulting Stop when the NAS ACKs — we do not
// optimistically delete it.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/hikrad/hikrad/internal/radius"
)

type disconnectRequest struct {
	NASID         string `json:"nas_id"`
	AcctSessionID string `json:"acct_session_id"`
}

func (m *Module) disconnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req disconnectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	if req.NASID == "" || req.AcctSessionID == "" {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "nas_id and acct_session_id are required")
		return
	}

	// Resolve the live session for its username/framed-IP and ownership.
	state, ok := m.liveSession(ctx, req.NASID, req.AcctSessionID)
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no such live session")
		return
	}
	if allowed, unscoped := allowedSubscribers(ctx, pkgDB, auth.ScopeFilter(ctx)); !unscoped {
		if state.SubscriberID == "" || !contains(allowed, state.SubscriberID) {
			httpapi.Error(w, http.StatusForbidden, "forbidden", "not permitted for this session")
			return
		}
	}

	outboxID, err := radius.EnqueueDisconnect(ctx, radius.SessionRef{
		NASID:         req.NASID,
		AcctSessionID: req.AcctSessionID,
		Username:      state.Username,
		FramedIP:      state.IP,
		Service:       state.Service,
	}, "manual.panel_disconnect", state.SubscriberID)
	if err != nil {
		m.internal(w, "enqueue disconnect", err)
		return
	}
	httpapi.JSON(w, http.StatusAccepted, map[string]any{"outbox_id": outboxID, "state": "pending"})
}

func (m *Module) liveSession(ctx context.Context, nasID, acctSessionID string) (livestate.State, bool) {
	if pkgRDB == nil {
		return livestate.State{}, false
	}
	raw, err := pkgRDB.HGet(ctx, livestate.HashKey, livestate.Field(nasID, acctSessionID)).Bytes()
	if err != nil {
		return livestate.State{}, false
	}
	s, err := livestate.Unmarshal(raw)
	if err != nil {
		return livestate.State{}, false
	}
	return s, true
}
