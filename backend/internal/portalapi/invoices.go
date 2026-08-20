package portalapi

// Portal invoices (FR-171, consuming v4-9's FR-154 via billing's portal
// seam). Read-only — paying an unpaid invoice reuses the EXISTING unified
// Pay screen (payments.go's submitTicketHandler); this file adds no second
// payment path, only the read surface FR-171 needs on top of it.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/billing"
	"github.com/hikrad/hikrad/internal/httpapi"
)

func (m *Module) listInvoicesHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	items, err := billing.PortalInvoices(r.Context(), sub.ID)
	if err != nil {
		m.internalError(w, "list portal invoices", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (m *Module) getInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	v, err := billing.PortalInvoice(r.Context(), sub.ID, chi.URLParam(r, "id"))
	if errors.Is(err, billing.ErrInvoiceNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if err != nil {
		m.internalError(w, "get portal invoice", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v)
}

func (m *Module) invoicePrintHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	body, err := billing.PortalInvoicePrintHTML(r.Context(), sub.ID, chi.URLParam(r, "id"))
	if errors.Is(err, billing.ErrInvoiceNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if err != nil {
		m.internalError(w, "invoice print", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}
