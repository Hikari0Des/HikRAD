package auth

// GET/PUT /api/v1/me/preferences (v2-6, FR-84.2, contract C2/C3). The route
// takes no id parameter — "another manager's preferences" is not an
// addressable resource through this endpoint at all, by construction.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/httpapi"
)

func getPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	m, ok := ManagerFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	p, err := GetPreferences(r.Context(), svc.db, m.ID)
	if err != nil {
		svc.log.Error("get preferences failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, p)
}

func putPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	m, ok := ManagerFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	var in Preferences
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if errs := validatePreferences(in); len(errs) > 0 {
		fe := make([]httpapi.FieldError, len(errs))
		for i, e := range errs {
			fe[i] = httpapi.FieldError{Field: e.Field, Message: e.Message}
		}
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	if err := SetPreferences(r.Context(), svc.db, m.ID, in); err != nil {
		svc.log.Error("set preferences failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, in)
}
