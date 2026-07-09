// Package radius serves the FreeRADIUS policy endpoint (contract C4):
// FreeRADIUS's rlm_rest calls POST /internal/radius/authorize for every
// Access-Request, and deploy/freeradius/ maps the response's abstract
// intents onto vendor VSAs (Mikrotik-Rate-Limit, Framed-Pool,
// Session-Timeout) — no vendor name ever appears in this package (FR-17
// vendor neutrality).
//
// Phase 1 is a stub (stub_policy.go): only the seeded testuser
// (backend/internal/seed) can accept, looked up live from the C6
// subscribers table so the packet harness exercises the real DB + AES-GCM
// decrypt path (NFR-4.2), for both PAP and CHAP. Phase 2 replaces
// stub_policy.go with the full policy engine; this file's HTTP wiring is
// expected to survive unchanged.
package radius

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
)

type Module struct{}

func (Module) Name() string { return "radius" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	// Deps (C3) carries no encryption key, so this loads it directly.
	// main() already called platform.LoadConfig() successfully before
	// building the router, so this is deterministic re-reading of the same
	// validated environment, not a new fallible step; failure here means the
	// environment changed underneath a running process, which deserves a
	// loud crash rather than a per-request 500.
	cfg, err := platform.LoadConfig()
	if err != nil {
		panic("radius: reload config: " + err.Error())
	}
	// Internal-only route: served on :8080 but never proxied by Caddy (C4).
	r.Post("/internal/radius/authorize", authorizeHandler(d, cfg.EncryptionKey))
}

func init() { httpapi.Add(Module{}) }

// Request/response shapes frozen by contract C4.
type authorizeRequest struct {
	Username         string `json:"username" validate:"required"`
	Password         string `json:"password"`
	ChapChallenge    string `json:"chap_challenge"`
	ChapResponse     string `json:"chap_response"`
	NasIP            string `json:"nas_ip" validate:"required"`
	CallingStationID string `json:"calling_station_id"`
	Service          string `json:"service" validate:"required,oneof=pppoe hotspot"`
}

type attribute struct {
	Intent string `json:"intent"`
	Value  string `json:"value"`
}

type authorizeResponse struct {
	Action     string      `json:"action"`
	Reason     string      `json:"reason"`
	Attributes []attribute `json:"attributes"`
}

func authorizeHandler(d httpapi.Deps, encKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req authorizeRequest
		if !httpapi.Bind(w, r, &req) {
			return
		}
		resp, err := decide(r.Context(), d.DB, encKey, req)
		if err != nil {
			d.Log.Error("radius authorize failed", "error", err, "username", req.Username)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		httpapi.JSON(w, http.StatusOK, resp)
	}
}
