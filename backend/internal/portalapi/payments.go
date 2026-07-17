package portalapi

// Portal renewal surface (v2-2 contracts C4/C5/C13, FR-42/78): voucher
// redeem, the unified Pay screen (pay-methods, ticket submission, latest
// ticket). Every handler resolves the subscriber id from the token only
// (IDOR rule) and delegates the actual money/lifecycle work to billing's
// exported seam (portal_seam.go) — every renewal here converges on the same
// single path panel/agent/voucher use.

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

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

// --- Unified Pay screen (v2-2 contracts C4/C5/C13, FR-78) -------------------

const maxTicketUploadBytes = 32 << 20 // 5 attachments * 10MB budget + form overhead (ticket_attachments.go enforces the real per-file/count caps)

// payMethodsHandler serves GET /portal/pay-methods (C4/C13): exactly what
// the subscriber's owning manager has enabled + configured, no fallback.
func (m *Module) payMethodsHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	methods, err := billing.ResolvePayMethods(r.Context(), sub.ID)
	if err != nil {
		m.internalError(w, "resolve pay methods", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": methods})
}

// submitTicketHandler serves POST /portal/payment-tickets (C5/C13): a
// multipart form (JSON-ish fields alongside file parts) so a subscriber can
// attach a transfer screenshot in the same request. Scratch-card submissions
// use the same route with only method_key/card_type/card_code set.
func (m *Module) submitTicketHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	if err := r.ParseMultipartForm(maxTicketUploadBytes); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "could not parse form")
		return
	}
	methodKey := r.FormValue("method_key")
	if methodKey == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "method_key", Message: "this field is required"})
		return
	}
	req := billing.SubmitTicketRequest{
		SubscriberID:      sub.ID,
		MethodKey:         methodKey,
		TransferReference: r.FormValue("transfer_reference"),
		Note:              r.FormValue("note"),
		CardType:          r.FormValue("card_type"),
		CardCode:          r.FormValue("card_code"),
	}
	if v := r.FormValue("amount"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			req.Amount = n
		}
	}
	if v := r.FormValue("transfer_date"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			req.TransferDate = &t
		}
	}
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["attachments"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(f, maxTicketUploadBytes))
			f.Close()
			if err != nil {
				continue
			}
			req.Attachments = append(req.Attachments, billing.UploadedFile{
				Filename: fh.Filename, ContentType: fh.Header.Get("Content-Type"), Data: data,
			})
		}
	}
	res, err := billing.SubmitTicket(r.Context(), req)
	if err != nil {
		writeTicketSubmitError(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"id": res.ID, "state": res.State, "trial_granted": res.TrialGranted, "trial_expires_at": res.TrialExpiresAt,
	})
}

// latestTicketHandler serves GET /portal/payment-tickets/latest (C13's
// "pending — under review" banner).
func (m *Module) latestTicketHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	v, ok, err := billing.LatestTicket(r.Context(), sub.ID)
	if err != nil {
		m.internalError(w, "latest payment ticket", err)
		return
	}
	if !ok {
		httpapi.JSON(w, http.StatusOK, nil)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"id": v.ID, "method_key": v.MethodKey, "state": v.State, "trial_granted": v.TrialGranted,
		"trial_expires_at": v.TrialExpiresAt, "reject_reason": v.RejectReason, "created_at": v.CreatedAt,
	})
}

func writeTicketSubmitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billing.ErrMethodNotAllowed):
		httpapi.Error(w, http.StatusForbidden, "method_not_allowed", "billing.error.method_not_allowed")
	case errors.Is(err, billing.ErrTicketPending):
		httpapi.Error(w, http.StatusConflict, "ticket_pending", "billing.error.ticket_pending")
	case errors.Is(err, billing.ErrCardTypeNotAllowed):
		httpapi.Error(w, http.StatusUnprocessableEntity, "card_code_invalid", "billing.error.card_code_invalid")
	case errors.Is(err, billing.ErrNoProfile):
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_profile", "subscriber has no profile to renew")
	case errors.Is(err, billing.ErrSubscriberNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
	default:
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}
