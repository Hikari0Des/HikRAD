package cards

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

func toFieldErrors(fe []fieldError) []httpapi.FieldError {
	out := make([]httpapi.FieldError, len(fe))
	for i, f := range fe {
		out[i] = httpapi.FieldError{Field: f.Field, Message: f.Message}
	}
	return out
}

func (m *Module) listGroupsHandler(w http.ResponseWriter, r *http.Request) {
	groups, err := listGroups(r.Context(), m.db)
	if err != nil {
		m.internalError(w, "list groups", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": groups})
}

func (m *Module) getGroupHandler(w http.ResponseWriter, r *http.Request) {
	g, err := getGroup(r.Context(), m.db, chi.URLParam(r, "id"))
	if err == errGroupNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card group not found")
		return
	}
	if err != nil {
		m.internalError(w, "get group", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, g)
}

func (m *Module) listCardsHandler(w http.ResponseWriter, r *http.Request) {
	cardsList, err := listCards(r.Context(), m.db, chi.URLParam(r, "id"))
	if err != nil {
		m.internalError(w, "list cards", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": cardsList})
}

// createGroupHandler creates a group and returns the plaintext codes for its
// initial cards — the only time they exist, exactly mirroring vouchers'
// generation response.
func (m *Module) createGroupHandler(w http.ResponseWriter, r *http.Request) {
	var in groupInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.validate(); len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", toFieldErrors(fe)...)
		return
	}
	validUntil, ferr := parseOptionalTime(in.ValidUntil)
	if ferr != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "valid_until", Message: "must be RFC3339"})
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	creatorID := ""
	if mgr != nil {
		creatorID = mgr.ID
	}
	g, codes, err := createGroup(r.Context(), m.db, in, creatorID, validUntil)
	if err != nil {
		m.internalError(w, "create group", err)
		return
	}
	_ = auth.Audit(r.Context(), "card_group.create", "card_group", g.ID, nil,
		map[string]any{"name": g.Name, "count": len(codes)})
	httpapi.JSON(w, http.StatusCreated, map[string]any{"group": g, "codes": codes})
}

func (m *Module) enableGroupHandler(w http.ResponseWriter, r *http.Request)  { m.setGroupEnabled(w, r, true) }
func (m *Module) disableGroupHandler(w http.ResponseWriter, r *http.Request) { m.setGroupEnabled(w, r, false) }

func (m *Module) setGroupEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id := chi.URLParam(r, "id")
	if err := setGroupEnabled(r.Context(), m.db, id, enabled); err == errGroupNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card group not found")
		return
	} else if err != nil {
		m.internalError(w, "set group enabled", err)
		return
	}
	action := "card_group.disable"
	if enabled {
		action = "card_group.enable"
	}
	_ = auth.Audit(r.Context(), action, "card_group", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
}

type extendRequest struct {
	ValidUntil string `json:"valid_until"`
}

func (m *Module) extendGroupHandler(w http.ResponseWriter, r *http.Request) {
	var in extendRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	t, err := time.Parse(time.RFC3339, in.ValidUntil)
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "valid_until", Message: "must be RFC3339"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := extendValidUntil(r.Context(), m.db, id, t); err == errGroupNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card group not found")
		return
	} else if err != nil {
		m.internalError(w, "extend group", err)
		return
	}
	_ = auth.Audit(r.Context(), "card_group.extend", "card_group", id, nil, map[string]any{"valid_until": in.ValidUntil})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "valid_until": in.ValidUntil})
}

type countRequest struct {
	Count int `json:"count"`
}

func (m *Module) addMoreHandler(w http.ResponseWriter, r *http.Request) {
	var in countRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Count < 1 || in.Count > 10000 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "count", Message: "must be between 1 and 10000"})
		return
	}
	id := chi.URLParam(r, "id")
	codes, err := addMoreCards(r.Context(), m.db, id, in.Count)
	if err == errGroupNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card group not found")
		return
	}
	if err != nil {
		m.internalError(w, "add more cards", err)
		return
	}
	_ = auth.Audit(r.Context(), "card_group.add_more", "card_group", id, nil, map[string]any{"count": len(codes)})
	httpapi.JSON(w, http.StatusCreated, map[string]any{"codes": codes})
}

// duplicateGroupHandler: count is optional (0 falls back to 1 card), but the
// request body must still be a JSON object — send {} for the default.
func (m *Module) duplicateGroupHandler(w http.ResponseWriter, r *http.Request) {
	var in countRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	sourceID := chi.URLParam(r, "id")
	g, codes, err := duplicateGroup(r.Context(), m.db, sourceID, in.Count)
	if err == errGroupNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card group not found")
		return
	}
	if err != nil {
		m.internalError(w, "duplicate group", err)
		return
	}
	_ = auth.Audit(r.Context(), "card_group.duplicate", "card_group", g.ID, nil,
		map[string]any{"source_id": sourceID, "count": len(codes)})
	httpapi.JSON(w, http.StatusCreated, map[string]any{"group": g, "codes": codes})
}

func (m *Module) resetCardHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := resetCard(r.Context(), m.db, id); err == errCardNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card not found")
		return
	} else if err != nil {
		m.internalError(w, "reset card", err)
		return
	}
	_ = auth.Audit(r.Context(), "card.reset", "card", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id})
}

func (m *Module) disableCardHandler(w http.ResponseWriter, r *http.Request) { m.setCardStateHandler(w, r, "disabled") }
func (m *Module) enableCardHandler(w http.ResponseWriter, r *http.Request)  { m.setCardStateHandler(w, r, "active") }

func (m *Module) setCardStateHandler(w http.ResponseWriter, r *http.Request, state string) {
	id := chi.URLParam(r, "id")
	if err := setCardState(r.Context(), m.db, id, state); err == errCardNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card not found")
		return
	} else if err != nil {
		m.internalError(w, "set card state", err)
		return
	}
	_ = auth.Audit(r.Context(), "card.set_state", "card", id, nil, map[string]any{"state": state})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": state})
}

func parseOptionalTime(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
