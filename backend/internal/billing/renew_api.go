package billing

// HTTP surface for the renewal path (C2). Resolves the actor + scope, honors the
// Idempotency-Key header, maps the renewal sentinels to conventional codes, and
// audits every renewal (before/after expiry) into the append-only log.

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type renewRequest struct {
	ProfileID string `json:"profile_id"`
	Note      string `json:"note"`
}

func (m *Module) renewHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())

	// Scope: a scoped agent may only renew their own subscribers (FR-27.2).
	if !m.subscriberVisible(r.Context(), id, auth.ScopeFilter(r.Context())) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}

	var in renewRequest
	if r.ContentLength != 0 && !httpapi.Bind(w, r, &in) {
		return
	}

	source := "panel"
	if mgr != nil && mgr.Scoped {
		source = "agent"
	}
	res, err := m.renew(r.Context(), renewParams{
		subscriberID:   id,
		profileID:      in.ProfileID,
		actorManagerID: managerID(mgr),
		source:         source,
		ledgerType:     "renewal",
		method:         "renewal",
		note:           in.Note,
		chargeBalance:  true,
		enforceBalance: m.enforceBalanceFor(r.Context(), mgr),
		idemKey:        r.Header.Get("Idempotency-Key"),
	})
	if m.writeRenewError(w, err) {
		return
	}

	_ = auth.Audit(r.Context(), "subscriber.renew", "subscriber", id, nil, map[string]any{
		"ledger_tx_id": res.LedgerTxID, "receipt_no": res.ReceiptNo,
		"new_expires_at": res.NewExpiresAt, "price": res.price, "currency": res.Currency, "coa_result": res.CoAResult,
	})
	httpapi.JSON(w, http.StatusOK, res)
}

// writeRenewError maps a renewal sentinel to the C2 envelope; returns true when
// it wrote an error (caller should stop). A nil error writes nothing.
func (m *Module) writeRenewError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errNoSubscriber):
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
	case errors.Is(err, errNoProfile):
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_profile", "subscriber has no profile to renew")
	case errors.Is(err, errProfileArchived):
		httpapi.Error(w, http.StatusUnprocessableEntity, "profile_archived", "the selected profile is archived")
	case errors.Is(err, errInsufficientFunds):
		// Localized message key for E (FR-20.1 clear message).
		httpapi.Error(w, http.StatusUnprocessableEntity, "insufficient_balance", "billing.error.insufficient_balance")
	default:
		m.internalError(w, "renew", err)
	}
	return true
}

// subscriberVisible reports whether the subscriber exists and is within the
// caller's scope (nil scope = unscoped/admin sees all).
func (m *Module) subscriberVisible(ctx context.Context, id string, scope *auth.ManagerScope) bool {
	q := `SELECT 1 FROM subscribers WHERE id = $1::uuid`
	args := []any{id}
	if scope != nil {
		q += ` AND owner_manager_id = $2::uuid`
		args = append(args, scope.ManagerID)
	}
	var one int
	err := m.db.QueryRow(ctx, q, args...).Scan(&one)
	return err == nil
}

// managerID returns the manager's id or "" (nil for internal/unauthenticated).
func managerID(mgr *auth.Manager) string {
	if mgr == nil {
		return ""
	}
	return mgr.ID
}
