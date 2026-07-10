// Package radius serves the FreeRADIUS policy endpoint (contract C4) and owns
// the NAS registry, IP pools, CoA service, vendor-adapter layer and RADIUS
// packet harness (Phase 2, Agent 2). FreeRADIUS's exec bridge calls POST
// /internal/radius/authorize for every Access-Request; deploy/freeradius/ maps
// the response's abstract intents onto the vendor's concrete VSAs (rate-limit,
// address-pool, …) — no vendor attribute name ever appears here (FR-17).
//
// The authorize decision is the full policy engine (policy.go): credentials
// (PAP+CHAP against AES-GCM-sealed passwords), status/expiry with per-profile
// behavior, quota, simultaneous-session limit, MAC lock, and FR-58 dual-service
// — all under the NFR-1 100 ms p99 budget using D's cached AuthView (C4) and
// C's live counts (C6), both injected to avoid an import cycle.
package radius

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/httpapi"
)

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

func (m *module) authorizeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req authorizeRequest
		if !httpapi.Bind(w, r, &req) {
			return
		}
		resp, err := m.eng.decide(r.Context(), req)
		if err != nil {
			m.log.Error("radius authorize failed", "error", err, "username", req.Username)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		httpapi.JSON(w, http.StatusOK, resp)
	}
}
