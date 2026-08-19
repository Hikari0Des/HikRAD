package notify

// Rules & timers tab (FR-162): per-rule enable/disable, timing, channels,
// throttling, and the "ignore opt-out" flag — VISIBLE here, never a hidden
// default (binding rule).

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

func handleListRules(w http.ResponseWriter, r *http.Request) {
	items, err := listRules(r.Context(), pkgDB)
	if err != nil {
		internalErr(w, "list rules", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func handlePutRule(w http.ResponseWriter, r *http.Request) {
	event := chi.URLParam(r, "event")
	var in RuleUpdate
	if !httpapi.Bind(w, r, &in) {
		return
	}
	rule, ok, err := updateRule(r.Context(), pkgDB, event, in)
	if err != nil {
		internalErr(w, "update rule", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no such notification rule")
		return
	}
	_ = auth.Audit(r.Context(), "notification_rule.update", "notification_rule", rule.ID, nil, map[string]any{
		"event_key": event, "enabled": rule.Enabled, "channels": rule.Channels,
		"days_before": rule.DaysBefore, "ignore_opt_out": rule.IgnoreOptOut,
	})
	httpapi.JSON(w, http.StatusOK, rule)
}
