package profiles

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live"
	"github.com/hikrad/hikrad/internal/radius"
)

// profileInput is the create/update request body (C7-D). Nullable fields are
// pointers: absent/null = inherit or unlimited.
type profileInput struct {
	Name                string  `json:"name"`
	PriceIQD            int64   `json:"price_iqd"`
	DurationDays        int     `json:"duration_days"`
	RateDownKbps        int     `json:"rate_down_kbps"`
	RateUpKbps          int     `json:"rate_up_kbps"`
	PoolID              *string `json:"pool_id"`
	SessionLimitDefault int     `json:"session_limit_default"`
	QuotaMode           string  `json:"quota_mode"`
	QuotaTotalBytes     *int64  `json:"quota_total_bytes"`
	QuotaDownBytes      *int64  `json:"quota_down_bytes"`
	QuotaUpBytes        *int64  `json:"quota_up_bytes"`
	ThrottleRate        *string `json:"throttle_rate"`
	ExpiryBehavior      string  `json:"expiry_behavior"`
	QuotaBehavior       string  `json:"quota_behavior"`
	HotspotRateDownKbps *int    `json:"hotspot_rate_down_kbps"`
	HotspotRateUpKbps   *int    `json:"hotspot_rate_up_kbps"`
	// Burst/priority segments (FR-11): optional abstract "rx/tx" intents.
	BurstRate      *string `json:"burst_rate"`
	BurstThreshold *string `json:"burst_threshold"`
	BurstTime      *string `json:"burst_time"`
	RatePriority   *string `json:"rate_priority"`
	MinRate        *string `json:"min_rate"`
	// Archived is only honored on update (create always makes an active profile).
	Archived bool `json:"archived"`
}

// validate performs the cross-field checks the struct tags cannot express and
// applies defaults for the enum fields.
func (in *profileInput) validate() []httpapi.FieldError {
	var fe []httpapi.FieldError
	add := func(f, m string) { fe = append(fe, httpapi.FieldError{Field: f, Message: m}) }

	if in.Name == "" {
		add("name", "this field is required")
	}
	if in.PriceIQD < 0 {
		add("price_iqd", "must not be negative")
	}
	if in.DurationDays < 1 {
		add("duration_days", "must be at least 1")
	}
	if in.RateDownKbps < 0 || in.RateUpKbps < 0 {
		add("rate_down_kbps", "rates must not be negative")
	}
	if in.SessionLimitDefault < 1 {
		in.SessionLimitDefault = 1
	}
	if in.QuotaMode == "" {
		in.QuotaMode = "unlimited"
	}
	switch in.QuotaMode {
	case "unlimited":
		// Clear any stray quota bytes so the view reads clean.
		in.QuotaTotalBytes, in.QuotaDownBytes, in.QuotaUpBytes = nil, nil, nil
	case "total":
		if in.QuotaTotalBytes == nil || *in.QuotaTotalBytes <= 0 {
			add("quota_total_bytes", "required and positive when quota_mode is 'total'")
		}
	case "split":
		if (in.QuotaDownBytes == nil || *in.QuotaDownBytes <= 0) &&
			(in.QuotaUpBytes == nil || *in.QuotaUpBytes <= 0) {
			add("quota_down_bytes", "at least one of down/up bytes is required when quota_mode is 'split'")
		}
	default:
		add("quota_mode", "must be one of: unlimited total split")
	}
	if in.ExpiryBehavior == "" {
		in.ExpiryBehavior = "block"
	}
	if in.ExpiryBehavior != "block" && in.ExpiryBehavior != "expired_pool" {
		add("expiry_behavior", "must be one of: block expired_pool")
	}
	if in.QuotaBehavior == "" {
		in.QuotaBehavior = "block"
	}
	switch in.QuotaBehavior {
	case "block", "throttle", "expired_pool":
	default:
		add("quota_behavior", "must be one of: block throttle expired_pool")
	}
	if in.QuotaBehavior == "throttle" && (in.ThrottleRate == nil || *in.ThrottleRate == "") {
		add("throttle_rate", "required when quota_behavior is 'throttle'")
	}
	return fe
}

func (m *Module) listHandler(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("archived") == "true"
	items, err := listProfiles(r.Context(), m.db, includeArchived)
	if err != nil {
		m.internalError(w, "list", err)
		return
	}
	// Non-paginated: the profile count is small (plans, not subscribers).
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (m *Module) getHandler(w http.ResponseWriter, r *http.Request) {
	p, err := getProfile(r.Context(), m.db, chi.URLParam(r, "id"))
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	if err != nil {
		m.internalError(w, "get", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p)
}

func (m *Module) createHandler(w http.ResponseWriter, r *http.Request) {
	var in profileInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.validate(); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	in.Archived = false
	p, err := insertProfile(r.Context(), m.db, in)
	if err != nil {
		m.internalError(w, "create", err)
		return
	}
	_ = auth.Audit(r.Context(), "profile.create", "profile", p.ID, nil, p)
	httpapi.JSON(w, http.StatusCreated, p)
}

// applyResult is the extra payload returned when an edit is applied immediately:
// the online sessions whose rate the panel may refresh via CoA (task 7 / FR-8).
type onlineRef struct {
	SubscriberID  string `json:"subscriber_id"`
	Username      string `json:"username"`
	NASID         string `json:"nas_id"`
	AcctSessionID string `json:"acct_session_id"`
	FramedIP      string `json:"framed_ip"`
}

func (m *Module) updateHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in profileInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.validate(); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	before, err := getProfile(r.Context(), m.db, id)
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	if err != nil {
		m.internalError(w, "get for update", err)
		return
	}
	after, err := updateProfile(r.Context(), m.db, id, in)
	if err != nil {
		m.internalError(w, "update", err)
		return
	}
	_ = auth.Audit(r.Context(), "profile.update", "profile", id, before, after)

	// apply=now (default) pushes the change to existing users immediately:
	// invalidate every affected subscriber's cached policy and surface the online
	// ones for a CoA rate refresh. apply=next_renewal only persists the row —
	// existing users pick it up on their next renewal (Phase 3).
	resp := map[string]any{"profile": after}
	if r.URL.Query().Get("apply") != "next_renewal" {
		online := m.applyNow(r.Context(), id, auth.ScopeFilter(r.Context()))
		resp["applied"] = "now"
		resp["online_affected"] = online
	} else {
		resp["applied"] = "next_renewal"
		resp["online_affected"] = []onlineRef{}
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

// applyNow invalidates B's policy cache for every subscriber on the profile and
// returns the online sessions (scoped) so E can offer a CoA rate refresh.
func (m *Module) applyNow(ctx context.Context, profileID string, scope *auth.ManagerScope) []onlineRef {
	ids, err := subscribersOnProfile(ctx, m.db, profileID)
	if err != nil {
		m.log.Error("profiles: load subscribers for apply-now", "error", err, "profile", profileID)
	}
	for _, sid := range ids {
		if err := radius.InvalidatePolicy(sid); err != nil {
			m.log.Warn("profiles: invalidate policy failed", "error", err, "subscriber", sid)
		}
	}
	out := []onlineRef{}
	sessions, err := live.List(ctx, live.Filter{ProfileID: profileID}, scope)
	if err != nil {
		m.log.Warn("profiles: list online for apply-now", "error", err, "profile", profileID)
		return out
	}
	for _, s := range sessions {
		out = append(out, onlineRef{
			SubscriberID:  s.SubscriberID,
			Username:      s.Username,
			NASID:         s.NASID,
			AcctSessionID: s.AcctSessionID,
			FramedIP:      s.IP,
		})
	}
	return out
}

func (m *Module) archiveHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	before, err := getProfile(r.Context(), m.db, id)
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	if err != nil {
		m.internalError(w, "get for archive", err)
		return
	}
	after, err := archiveProfile(r.Context(), m.db, id)
	if err != nil {
		m.internalError(w, "archive", err)
		return
	}
	_ = auth.Audit(r.Context(), "profile.archive", "profile", id, before, after)
	httpapi.JSON(w, http.StatusOK, after)
}
