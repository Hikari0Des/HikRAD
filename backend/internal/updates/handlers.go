package updates

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

// notConfigured is the FR-87 "the daemon was never provisioned on this
// install" response — distinct from "provisioned but unreachable" (a real
// error, also 503, but a different message pointing at a different fix).
func notConfigured(w http.ResponseWriter) {
	httpapi.Error(w, http.StatusServiceUnavailable, "not_configured",
		"the one-click updater is not provisioned on this install — run install.sh once (source, --bundle, or the repair option) to enable it")
}

func daemonUnreachable(w http.ResponseWriter, err error) {
	httpapi.Error(w, http.StatusServiceUnavailable, "daemon_unreachable", "could not reach hikrad-updaterd: "+err.Error())
}

func checkHandler(w http.ResponseWriter, r *http.Request) {
	if !configured() {
		notConfigured(w)
		return
	}
	var resp checkResponse
	if err := oneShot("check", &resp); err != nil {
		daemonUnreachable(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

type mergedStatus struct {
	OK          bool    `json:"ok"`
	Locked      bool    `json:"locked"`
	LockOwner   *string `json:"lock_owner"`
	Stage       string  `json:"stage"`
	StartedAt   *string `json:"started_at"`
	LastAction  string  `json:"last_action,omitempty"`
	Result      string  `json:"result,omitempty"`
	Version     string  `json:"version,omitempty"`
	CompletedAt string  `json:"completed_at,omitempty"`
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	if !configured() {
		notConfigured(w)
		return
	}
	var st statusResponse
	if err := oneShot("status", &st); err != nil {
		daemonUnreachable(w, err)
		return
	}
	resp := mergedStatus{OK: st.OK, Locked: st.Locked, LockOwner: st.LockOwner, Stage: st.Stage, StartedAt: st.StartedAt}
	if !st.Locked {
		// C4: "additionally folds in rollback-status's fields when stage is
		// idle" — this is what lets the panel answer "did the update I lost
		// the stream for actually finish" from one endpoint.
		var rb rollbackStatusResponse
		if err := oneShot("rollback-status", &rb); err == nil {
			resp.LastAction, resp.Result, resp.Version, resp.CompletedAt = rb.LastAction, rb.Result, rb.Version, rb.CompletedAt
		}
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	if !configured() {
		notConfigured(w)
		return
	}

	var body struct {
		BundlePath string `json:"bundle_path"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			httpapi.Error(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
			return
		}
	}

	requester := "socket"
	if mgr, ok := auth.ManagerFrom(r.Context()); ok && mgr != nil {
		requester = "panel:" + mgr.ID
	}

	conn, err := dial()
	if err != nil {
		daemonUnreachable(w, err)
		return
	}
	if err := sendRequest(conn, request{Verb: "update", BundlePath: body.BundlePath, Requester: requester}); err != nil {
		conn.Close()
		daemonUnreachable(w, err)
		return
	}

	// The daemon writes nothing until it knows whether this is a real run or
	// a lock conflict (up to its own 30s resolve timeout, C2) — 35s here is
	// a safety ceiling above that, not the expected latency.
	_ = conn.SetReadDeadline(time.Now().Add(35 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		conn.Close()
		daemonUnreachable(w, err)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	trimmed := strings.TrimSpace(line)

	// An ErrorResponse ({"ok":false,"error":...}) and an Event
	// ({"event":"stage"|"result",...}) are both valid JSON objects but
	// distinguishable by which key is present (C2) — an Event's own
	// optional "ok" field (on a "result" line) is why this checks for
	// "event" specifically rather than the presence/absence of "ok".
	var probe struct {
		Event string `json:"event"`
	}
	_ = json.Unmarshal([]byte(trimmed), &probe)

	if probe.Event == "" {
		var errResp errorResponse
		_ = json.Unmarshal([]byte(trimmed), &errResp)
		conn.Close()
		switch errResp.Error {
		case "locked":
			httpapi.Error(w, http.StatusConflict, "locked", "an update is already in progress")
		case "invalid bundle_path":
			httpapi.Error(w, http.StatusBadRequest, "invalid_bundle_path", "bundle_path is not a valid drop-directory file")
		case "unauthorized":
			httpapi.Error(w, http.StatusServiceUnavailable, "daemon_error", "hikrad-updaterd rejected the relay's own token — HIKRAD_UPDATER_TOKEN must match on both sides")
		default:
			httpapi.Error(w, http.StatusServiceUnavailable, "daemon_error", "hikrad-updaterd refused the request")
		}
		return
	}

	// A real run started (or, rarely, failed immediately with a single
	// "result" line — e.g. the child binary itself couldn't exec — either
	// way it's a genuine Event stream, not a lock/validation rejection):
	// hand the still-open connection to a relayRun the SSE handler can
	// attach to (C4's "hikrad-api holds one persistent connection... opened
	// either by the POST above or a fresh one on reconnect").
	var first event
	_ = json.Unmarshal([]byte(trimmed), &first)
	rr := &relayRun{doneCh: make(chan struct{})}
	rr.append(first)
	currentMu.Lock()
	current = rr
	currentMu.Unlock()
	go rr.pump(conn, br)

	// FR-87.3: audit the request itself, not the eventual outcome (the
	// daemon/relay stream is the record of what actually happened).
	_ = auth.Audit(r.Context(), "update.start", "system", "", nil, map[string]any{
		"bundle_path": body.BundlePath, "requester": requester,
	})

	httpapi.JSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}
