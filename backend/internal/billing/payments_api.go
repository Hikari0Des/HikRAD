package billing

// HTTP surface for the gateway layer (contract C3): the public webhook
// callback, admin gateway config, and the mock adapter's dev-only simulator.
// The portal-facing create/poll/list-gateways routes are exposed to
// subscribers via portalapi (Phase 4 Agent 3's other package) through the
// exported wrappers in portal_seam.go — they are NOT registered here because
// they need portal-token auth, which this package does not implement.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

// paymentCallbackHandler is the single public, unauthenticated, signature-
// verified webhook every gateway posts to (C3). Rate limiting lives in Caddy
// (NFR-4.4 perimeter) same as every other public route.
func (m *Module) paymentCallbackHandler(w http.ResponseWriter, r *http.Request) {
	gwName := chi.URLParam(r, "gateway")
	gw, err := m.resolveGatewayForCallback(r.Context(), gwName)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, "unknown_gateway", "unknown or unconfigured gateway")
		return
	}
	result, err := gw.VerifyCallback(r.Context(), r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_callback", "callback signature verification failed")
		return
	}
	if err := m.processCallback(r.Context(), gwName, result); err != nil {
		switch err {
		case errUnknownIntent:
			httpapi.Error(w, http.StatusNotFound, "unknown_intent", "unknown payment intent")
		case errAmountMismatch:
			httpapi.Error(w, http.StatusUnprocessableEntity, "amount_mismatch", "callback amount does not match the payment intent")
		case errTerminalIntent:
			// Already failed/expired: ack without error — nothing more to do.
			w.WriteHeader(http.StatusNoContent)
		default:
			m.internalError(w, "payment callback", err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Admin gateway config (C3: "expose the API now"; Phase-5 wires the UI) -

func (m *Module) listGatewayConfigsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := m.listGatewayConfigs(r.Context())
	if err != nil {
		m.internalError(w, "list gateway configs", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type putGatewayConfigRequest struct {
	Enabled bool           `json:"enabled"`
	Mode    string         `json:"mode"`
	Creds   map[string]any `json:"creds,omitempty" audit:"secret"`
}

func (m *Module) putGatewayConfigHandler(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "gateway")
	var in putGatewayConfigRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if err := m.putGatewayConfig(r.Context(), name, in.Enabled, in.Mode, in.Creds); err != nil {
		m.internalError(w, "put gateway config", err)
		return
	}
	_ = auth.Audit(r.Context(), "payment_gateway.configure", "gateway", name, nil, in)
	httpapi.JSON(w, http.StatusOK, map[string]any{"gateway": name, "enabled": in.Enabled})
}

// --- Mock adapter dev-only simulator (approve/fail/delay) -----------------
// Used by the gate, CI, and F's portal development to drive the full
// lifecycle (including a real signature-verified callback) with no browser.
// It only ever affects gateway="mock" state; there is nothing here that can
// touch a live gateway.

type simulateRequest struct {
	GatewayRef string `json:"gateway_ref"`
	Action     string `json:"action"` // approve|fail|delay
}

func (m *Module) mockSimulateHandler(w http.ResponseWriter, r *http.Request) {
	var in simulateRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.GatewayRef == "" || in.Action == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "gateway_ref", Message: "gateway_ref and action are required"})
		return
	}
	payload, err := mockGW.Simulate(in.GatewayRef, in.Action)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, "unknown_gateway_ref", err.Error())
		return
	}
	if payload == nil { // "delay": observe-only, nothing to process
		httpapi.JSON(w, http.StatusOK, map[string]any{"observed": true})
		return
	}
	var cb struct {
		OrderID    string `json:"order_id"`
		GatewayRef string `json:"gateway_ref"`
		AmountIQD  int64  `json:"amount_iqd"`
		State      string `json:"state"`
	}
	if err := json.Unmarshal(payload, &cb); err != nil {
		m.internalError(w, "mock simulate decode", err)
		return
	}
	if err := m.processCallback(r.Context(), "mock", callbackResultFrom(cb.OrderID, cb.GatewayRef, cb.State, cb.AmountIQD)); err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "simulate_failed", err.Error())
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"order_id": cb.OrderID, "state": cb.State})
}
