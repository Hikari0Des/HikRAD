package notify

// Messages tab (FR-162): editable per-event templates, trilingual, versioned.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

func handleListTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := listCurrentTemplates(r.Context(), pkgDB)
	if err != nil {
		internalErr(w, "list templates", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func handleTemplateHistory(w http.ResponseWriter, r *http.Request) {
	event := chi.URLParam(r, "event")
	locale := chi.URLParam(r, "locale")
	items, err := listTemplateHistory(r.Context(), pkgDB, event, locale)
	if err != nil {
		internalErr(w, "template history", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type putTemplateRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// handlePutTemplate serves PUT /notifications/templates/{event}/{locale} —
// always inserts the NEXT version (append-only, gate item 6: the outbox
// keeps identifying exactly the text that produced a given message).
func handlePutTemplate(w http.ResponseWriter, r *http.Request) {
	event := chi.URLParam(r, "event")
	locale := chi.URLParam(r, "locale")
	var in putTemplateRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Body == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "body is required",
			httpapi.FieldError{Field: "body", Message: "required"})
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	createdBy := ""
	if mgr != nil {
		createdBy = mgr.ID
	}
	t, err := createTemplateVersion(r.Context(), pkgDB, event, locale, in.Subject, in.Body, createdBy)
	if err != nil {
		internalErr(w, "create template version", err)
		return
	}
	_ = auth.Audit(r.Context(), "notification_template.update", "notification_template", t.ID, nil,
		map[string]any{"event_key": event, "locale": locale, "version": t.Version})
	httpapi.JSON(w, http.StatusOK, t)
}
