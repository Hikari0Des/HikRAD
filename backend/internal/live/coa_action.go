package live

// Disconnect action (contract C5 consumer / FR-31.3). The panel's Disconnect
// button posts here; this handler resolves the live session, enforces manager
// scope, then calls B's CoA service (radius.Disconnect), which audits the
// attempt. The live row is removed by the resulting Stop when the NAS ACKs — we
// do not optimistically delete it.

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

	res := radius.Disconnect(ctx, radius.SessionRef{
		NASID:         req.NASID,
		AcctSessionID: req.AcctSessionID,
		Username:      state.Username,
		FramedIP:      state.IP,
	})
	status := http.StatusOK
	if !res.Ok() {
		// The CoA reached a decision but the NAS did not ACK (NAK/timeout): 502
		// so the panel can surface "the router refused / did not answer".
		status = http.StatusBadGateway
	}
	body := map[string]any{"outcome": string(res.Outcome)}
	if res.Err != nil {
		body["error"] = res.Err.Error()
	}
	httpapi.JSON(w, status, body)
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
