package portalapi

// Portal renewal surface (contracts C3, C8, FR-42): voucher redeem, e-wallet
// gateway create/poll, scratch-card submission. Every handler resolves the
// subscriber id from the token only (IDOR rule) and delegates the actual
// money/lifecycle work to billing's exported seam (portal_seam.go) — every
// renewal here converges on the same single path panel/agent/voucher use.

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/billing"
	"github.com/hikrad/hikrad/internal/httpapi"
)

// --- Payment history (C2 FR-41.3) ------------------------------------------

func (m *Module) paymentsHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	items, next, err := billing.PortalPayments(r.Context(), sub.ID, page)
	if err != nil {
		m.internalError(w, "portal payments", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}

// --- Voucher redemption (FR-42, sub-PRD 05 FR-22.3) ------------------------

type redeemRequest struct {
	Code string `json:"code" validate:"required"`
}

func (m *Module) redeemVoucherHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	var in redeemRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	res, outcome, err := billing.RedeemVoucher(r.Context(), in.Code, sub.ID)
	if err != nil {
		m.internalError(w, "voucher redeem", err)
		return
	}
	switch outcome {
	case billing.RedeemOK:
		httpapi.JSON(w, http.StatusOK, res)
	case billing.RedeemUsed:
		httpapi.Error(w, http.StatusUnprocessableEntity, "voucher_used", "billing.error.voucher_used")
	case billing.RedeemExpired:
		httpapi.Error(w, http.StatusUnprocessableEntity, "voucher_expired", "billing.error.voucher_expired")
	case billing.RedeemBatchVoid:
		httpapi.Error(w, http.StatusUnprocessableEntity, "voucher_void", "billing.error.voucher_void")
	default:
		httpapi.Error(w, http.StatusNotFound, "voucher_invalid", "billing.error.voucher_invalid")
	}
}

// --- E-wallet gateway (contract C3) -----------------------------------------

type gatewayItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (m *Module) listGatewaysHandler(w http.ResponseWriter, r *http.Request) {
	names, err := billing.ListEnabledGateways(r.Context())
	if err != nil {
		m.internalError(w, "list gateways", err)
		return
	}
	items := make([]gatewayItem, len(names))
	for i, n := range names {
		items[i] = gatewayItem{ID: n, Name: gatewayDisplayName(n)}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func gatewayDisplayName(id string) string {
	switch id {
	case "mock":
		return "Mock Gateway (demo)"
	case "zaincash":
		return "ZainCash"
	default:
		return id
	}
}

type createPaymentRequest struct {
	ProfileID string `json:"profile_id,omitempty"`
}

func (m *Module) createPaymentHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	gateway := chi.URLParam(r, "gateway")
	var in createPaymentRequest
	if r.ContentLength != 0 && !httpapi.Bind(w, r, &in) {
		return
	}
	id, redirectURL, err := billing.CreatePaymentIntent(r.Context(), sub.ID, gateway, in.ProfileID)
	if err != nil {
		writePaymentError(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"redirect_url": redirectURL, "intent_id": id})
}

func (m *Module) getPaymentIntentHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	id := chi.URLParam(r, "id")
	v, err := billing.GetPaymentIntent(r.Context(), id, sub.ID)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, "not_found", "payment intent not found")
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"id": v.ID, "gateway": v.Gateway, "state": v.State, "amount_iqd": v.AmountIQD,
		"gateway_ref": v.GatewayRef, "new_expires_at": v.NewExpiresAt,
	})
}

func writePaymentError(w http.ResponseWriter, err error) {
	switch err {
	case billing.ErrGatewayUnavailable:
		// Graceful degradation (NFR-7): the gateway is off/unreachable; the
		// portal falls back to voucher, which remains available regardless.
		httpapi.Error(w, http.StatusServiceUnavailable, "gateway_unavailable", "billing.error.gateway_unavailable")
	case billing.ErrNoProfile:
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_profile", "subscriber has no profile to renew")
	case billing.ErrProfileArchived:
		httpapi.Error(w, http.StatusUnprocessableEntity, "profile_archived", "the selected profile is archived")
	case billing.ErrSubscriberNotFound:
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
	default:
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}

// --- Scratch-card payments (C8, FR-59) --------------------------------------

type cardTypeItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (m *Module) cardTypesHandler(w http.ResponseWriter, r *http.Request) {
	ids := billing.CardTypes(r.Context())
	items := make([]cardTypeItem, len(ids))
	for i, id := range ids {
		items[i] = cardTypeItem{ID: id, Name: cardDisplayName(id)}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func cardDisplayName(id string) string {
	switch id {
	case "zain":
		return "Zain Cash Card"
	case "asiacell":
		return "Asiacell Card"
	default:
		return id
	}
}

func (m *Module) myCardPaymentHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	v, ok, err := billing.LatestCardPayment(r.Context(), sub.ID)
	if err != nil {
		m.internalError(w, "my card payment", err)
		return
	}
	if !ok {
		httpapi.JSON(w, http.StatusOK, nil)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"id": v.ID, "card_type": v.CardType, "state": v.State,
		"trial_expires_at": v.TrialExpiresAt, "reject_reason": v.RejectReason, "created_at": v.CreatedAt,
	})
}

type submitCardRequest struct {
	CardType string `json:"card_type" validate:"required"`
	Code     string `json:"code" validate:"required"`
}

func (m *Module) submitCardHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	var in submitCardRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	res, err := billing.SubmitCard(r.Context(), sub.ID, in.CardType, in.Code)
	if err != nil {
		writeCardSubmitError(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"state": res.State, "trial_expires_at": res.TrialExpiresAt})
}

func writeCardSubmitError(w http.ResponseWriter, err error) {
	var cd *billing.CardCooldownError
	switch {
	case errors.Is(err, billing.ErrCardPending):
		httpapi.Error(w, http.StatusConflict, "card_payment_pending", "a card payment is already pending")
	case errors.As(err, &cd):
		httpapi.Error(w, http.StatusUnprocessableEntity, "card_payment_cooldown",
			"card submissions are blocked until "+cd.RetryAt.Format(time.RFC3339))
	case errors.Is(err, billing.ErrCardTypeNotAllowed):
		httpapi.Error(w, http.StatusUnprocessableEntity, "card_code_invalid", "unsupported card type")
	case errors.Is(err, billing.ErrNoProfile):
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_profile", "subscriber has no profile to renew")
	case errors.Is(err, billing.ErrSubscriberNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
	default:
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}
