package subscribers

// Reset MAC (FR-5.2, AC-5a): the one-click "customer changed router" fix. Clears
// learned_mac so a learn-mode subscriber re-learns on the next auth. Audited and
// policy-invalidated like any auth-affecting mutation.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
)

func (m *Module) resetMacHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	scope := auth.ScopeFilter(r.Context())
	before, err := getByID(r.Context(), m.db, id, scope)
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}
	if err != nil {
		m.internalError(w, "get for reset-mac", err)
		return
	}
	after, err := scanSubscriber(m.db.QueryRow(r.Context(),
		`UPDATE subscribers SET learned_mac = NULL WHERE id = $1::uuid RETURNING `+columns, id))
	if err != nil {
		m.internalError(w, "reset-mac", err)
		return
	}
	_ = auth.Audit(r.Context(), "subscriber.reset_mac", "subscriber", id, before, after)
	_ = radius.InvalidatePolicy(id)
	httpapi.JSON(w, http.StatusOK, after)
}
