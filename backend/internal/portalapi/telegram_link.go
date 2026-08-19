package portalapi

// Telegram account-linking code issuance (FR-165): the backend half this
// phase delivers. The portal BUTTON that calls this is v4-11's UI (out of
// scope here per the phase brief); this endpoint exists now so v4-11 has
// something to consume, same posture as every other "this phase exposes,
// the next phase surfaces" seam in the program.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/telegrambot"
)

func (m *Module) telegramLinkCodeHandler(w http.ResponseWriter, r *http.Request) {
	sub, ok := SubscriberFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	code, expiresAt, err := telegrambot.IssueLinkCode(r.Context(), m.db, sub.ID)
	if err != nil {
		m.internalError(w, "issue telegram link code", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"code": code, "expires_at": expiresAt})
}
