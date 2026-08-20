package portalapi

// Portal support tickets (FR-170, consuming v4-10's FR-164 seam — see
// internal/support/public.go, whose doc comment literally says "the future
// portal HTTP surface (v4-11) wraps these identically"). This file is that
// wrap: thin auth-and-shape only, identity from the token (IDOR rule), no
// business logic. Internal notes cannot leak here because they cannot leak
// out of internal/support/public.go itself (PublicMessage has no field one
// could occupy) — this file adds no second boundary to get wrong.

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/support"
)

func (m *Module) listTicketsHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	items, err := support.ListForSubscriber(r.Context(), m.db, sub.ID)
	if err != nil {
		m.internalError(w, "list tickets", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (m *Module) getTicketHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	detail, ok, err := support.GetForSubscriber(r.Context(), m.db, chi.URLParam(r, "id"), sub.ID)
	if err != nil {
		m.internalError(w, "get ticket", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "ticket not found")
		return
	}
	httpapi.JSON(w, http.StatusOK, detail)
}

type createTicketRequest struct {
	Subject string `json:"subject" validate:"required"`
	Body    string `json:"body,omitempty"`
}

func (m *Module) createTicketHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	var in createTicketRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	t, err := support.CreateForSubscriber(r.Context(), m.db, sub.ID, strings.TrimSpace(in.Subject), strings.TrimSpace(in.Body), "subscriber")
	if err != nil {
		m.internalError(w, "create ticket", err)
		return
	}
	_ = auth.AuditActor(r.Context(), sub.ID, sub.IP, sub.UA, "portal.ticket.create", "support_ticket", t.ID, nil, map[string]any{"subject": t.Subject})
	httpapi.JSON(w, http.StatusCreated, t)
}

type replyTicketRequest struct {
	Body string `json:"body" validate:"required"`
}

func (m *Module) replyTicketHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	var in replyTicketRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	msg, ok, err := support.ReplyAsSubscriber(r.Context(), m.db, chi.URLParam(r, "id"), sub.ID, strings.TrimSpace(in.Body))
	if err != nil {
		m.internalError(w, "reply ticket", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "ticket not found")
		return
	}
	httpapi.JSON(w, http.StatusOK, msg)
}

func (m *Module) withdrawTicketHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := support.WithdrawForSubscriber(r.Context(), m.db, id, sub.ID); err != nil {
		m.internalError(w, "withdraw ticket", err)
		return
	}
	_ = auth.AuditActor(r.Context(), sub.ID, sub.IP, sub.UA, "portal.ticket.withdraw", "support_ticket", id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}
