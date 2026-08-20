package portalapi

// Portal session history (FR-166), consuming internal/live's subscriber-
// scoped seam (live.SessionsForSubscriber) — identity from the token only.

import (
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live"
)

func (m *Module) sessionHistoryHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	var cursorTS *time.Time
	var cursorID *string
	if len(page.Cursor) == 2 {
		t, terr := time.Parse(time.RFC3339Nano, page.Cursor[0])
		if terr != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
			return
		}
		cursorTS, cursorID = &t, &page.Cursor[1]
	} else if page.Cursor != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
		return
	}

	items, lastSort, lastID, hasMore, err := live.SessionsForSubscriber(r.Context(), m.db, sub.ID, cursorTS, cursorID, page.Limit)
	if err != nil {
		m.internalError(w, "portal session history", err)
		return
	}
	next := ""
	if hasMore {
		next = httpapi.EncodeCursor(lastSort.UTC().Format(time.RFC3339Nano), lastID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}
