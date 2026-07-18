// Package updates is hikrad-api's relay to hikrad-updaterd, the host-side
// update daemon (v2 phase 7, FR-86/87). It never runs an update itself and
// never touches the DB for update logic — it is a thin, token-authenticated
// pass-through over a unix socket, plus the in-memory SSE fan-out and
// permission/audit layer a bare socket client can't provide on its own.
//
// The wire types below intentionally duplicate backend/cmd/hikrad-updaterd's
// own protocol.go rather than importing it: hikrad-updaterd is a standalone,
// dependency-free host binary by design (docs/v2/phases/
// phase-v2-7-one-click-update/00-phase.md, C1), and it cannot import
// anything from internal/ without pulling that posture apart. Keep the two
// in sync by hand — the frozen contract (C2) is the source of truth for
// both.
package updates

import "os"

type request struct {
	Verb       string `json:"verb"`
	Token      string `json:"token"`
	BundlePath string `json:"bundle_path,omitempty"`
	Requester  string `json:"requester,omitempty"`
}

// event mirrors one line of the daemon's `update` progress stream.
type event struct {
	Type    string `json:"event"` // "stage" | "result"
	Stage   string `json:"stage,omitempty"`
	TS      string `json:"ts,omitempty"`
	OK      *bool  `json:"ok,omitempty"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
}

type checkResponse struct {
	OK               bool    `json:"ok"`
	CurrentVersion   string  `json:"current_version"`
	AvailableVersion *string `json:"available_version"`
	DeliveryMode     string  `json:"delivery_mode"`
	BundlePath       *string `json:"bundle_path"`
}

type statusResponse struct {
	OK        bool    `json:"ok"`
	Locked    bool    `json:"locked"`
	LockOwner *string `json:"lock_owner"`
	Stage     string  `json:"stage"`
	StartedAt *string `json:"started_at"`
}

type rollbackStatusResponse struct {
	OK          bool   `json:"ok"`
	LastAction  string `json:"last_action"`
	Result      string `json:"result"`
	Version     string `json:"version"`
	CompletedAt string `json:"completed_at"`
}

type errorResponse struct {
	OK        bool    `json:"ok"`
	Error     string  `json:"error"`
	LockOwner *string `json:"lock_owner,omitempty"`
	StartedAt *string `json:"started_at,omitempty"`
}

// socketPath/authToken are read fresh on every call (cheap env lookups, and
// it means a repair-installed token takes effect on the container's next
// request without a code change or restart-timing dependency).
func socketPath() string {
	if v := os.Getenv("HIKRAD_UPDATER_SOCKET"); v != "" {
		return v
	}
	return "/var/run/hikrad-updater/updater.sock"
}

func authToken() string { return os.Getenv("HIKRAD_UPDATER_TOKEN") }

// configured reports whether this install has ever provisioned the daemon
// (FR-87's "not configured" boundary — compose.yml leaves the token empty
// rather than failing hikrad-api's boot when it's absent, see deploy/
// compose.yml's own comment).
func configured() bool { return authToken() != "" }
