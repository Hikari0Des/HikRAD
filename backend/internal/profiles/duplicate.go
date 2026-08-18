package profiles

// Plan ergonomics (FR-132): one-click duplication and auto-renew bulk
// propagation. Both are single-purpose actions rather than routed through
// the FR-111 bulk-job machinery (bulk.go) — duplication targets exactly one
// profile and propagation is one bounded UPDATE, neither needs an async job
// id/progress-poll contract designed for hundreds of rows.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
)

// duplicateHandler copies a profile — its full row (limits, exhaust actions,
// burst/rate fields, address list), its FR-64 NAS scopes, and its FR-131
// speed windows — into a new row named "<name> (copy)", archived=false
// regardless of the source. Nothing subscriber-facing is copied (no
// subscribers are attached to the new plan).
func (m *Module) duplicateHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	src, err := getProfile(r.Context(), m.db, id)
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	if err != nil {
		m.internalError(w, "get for duplicate", err)
		return
	}

	in := profileInput{
		Name: src.Name + " (copy)", Price: src.Price, Currency: src.Currency,
		DurationDays: src.DurationDays, RateDownKbps: src.RateDownKbps, RateUpKbps: src.RateUpKbps,
		PoolID: src.PoolID, SessionLimitDefault: src.SessionLimitDefault,
		QuotaMode: src.QuotaMode, QuotaTotalBytes: src.QuotaTotalBytes,
		QuotaDownBytes: src.QuotaDownBytes, QuotaUpBytes: src.QuotaUpBytes,
		ThrottleRate: src.ThrottleRate, ExpiryBehavior: src.ExpiryBehavior, QuotaBehavior: src.QuotaBehavior,
		HotspotRateDownKbps: src.HotspotRateDownKbps, HotspotRateUpKbps: src.HotspotRateUpKbps,
		BurstRate: src.BurstRate, BurstThreshold: src.BurstThreshold, BurstTime: src.BurstTime,
		RatePriority: src.RatePriority, MinRate: src.MinRate, AddressList: src.AddressList,
		NASScopes: src.NASScopes,
		UptimeLimitSeconds: src.UptimeLimitSeconds, DailyLimitBytes: src.DailyLimitBytes,
		DailyResetMin: src.DailyResetMin,
		ExpiryExhaustTarget: src.ExpiryExhaustTarget, QuotaExhaustTarget: src.QuotaExhaustTarget,
		UptimeExhaustAction: src.UptimeExhaustAction, UptimeExhaustTarget: src.UptimeExhaustTarget,
		DailyExhaustAction: src.DailyExhaustAction, DailyExhaustTarget: src.DailyExhaustTarget,
		AutoRenewDefault: src.AutoRenewDefault,
	}
	if fe := in.validate(); fe != nil {
		// The source profile was valid when saved; this only trips if a
		// switch_plan target it referenced was since deleted — surfaced
		// rather than silently dropped.
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "begin duplicate", err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	out, err := insertProfile(r.Context(), tx, in)
	if err != nil {
		m.internalError(w, "duplicate", err)
		return
	}
	if err := radius.ReplaceScopes(r.Context(), tx, radius.ProfileScopes, out.ID, radius.DedupeScopes(src.NASScopes)); err != nil {
		m.internalError(w, "duplicate nas scopes", err)
		return
	}
	if err := duplicateSpeedWindows(r.Context(), tx, src.ID, out.ID); err != nil {
		m.internalError(w, "duplicate speed windows", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "commit duplicate", err)
		return
	}
	out.NASScopes = orEmptyScopes(src.NASScopes)
	_ = auth.Audit(r.Context(), "profile.duplicate", "profile", out.ID, map[string]any{"source_profile_id": src.ID}, out)
	httpapi.JSON(w, http.StatusCreated, out)
}

// propagateAutoRenewHandler sets subscribers.auto_renew = the profile's
// current auto_renew_default for every subscriber already on the plan
// (FR-132's "bulk propagation") — an explicit, one-click action, never a
// side effect of a plain profile save, so an operator changing the default
// for FUTURE subscribers never silently rewrites existing ones' preference.
func (m *Module) propagateAutoRenewHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := getProfile(r.Context(), m.db, id)
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	if err != nil {
		m.internalError(w, "get for propagate", err)
		return
	}
	ct, err := m.db.Exec(r.Context(),
		`UPDATE subscribers SET auto_renew = $2 WHERE profile_id = $1::uuid AND auto_renew IS DISTINCT FROM $2`,
		id, p.AutoRenewDefault)
	if err != nil {
		m.internalError(w, "propagate auto-renew", err)
		return
	}
	_ = auth.Audit(r.Context(), "profile.propagate_auto_renew", "profile", id, nil, map[string]any{
		"auto_renew_default": p.AutoRenewDefault, "subscribers_updated": ct.RowsAffected(),
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{"subscribers_updated": ct.RowsAffected()})
}
