package profiles

import (
	"context"
	"fmt"
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
	// NASScopes lists every NAS / service instance this profile's subscribers may
	// authenticate on (FR-64 / C9). An empty array (or omitted) = any NAS. The
	// set is replaced wholesale.
	NASScopes []radius.NASScope `json:"nas_scopes"`
	// Archived is only honored on update (create always makes an active profile).
	Archived bool `json:"archived"`
}

// nasScopeChanged reports whether an update moved the profile's FR-64 scope set.
// It decides whether the cached AuthViews MUST be invalidated even under
// ?apply=next_renewal — see updateHandler.
//
// Order-insensitive: the scope set is a set, and the panel may send it in any
// order. Comparing slices positionally would report a spurious change on every
// save and invalidate every subscriber's view for nothing.
func nasScopeChanged(before Profile, after Profile) bool {
	if len(before.NASScopes) != len(after.NASScopes) {
		return true
	}
	seen := make(map[radius.NASScope]int, len(before.NASScopes))
	for _, s := range before.NASScopes {
		seen[s]++
	}
	for _, s := range after.NASScopes {
		seen[s]--
		if seen[s] < 0 {
			return true
		}
	}
	return false
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

	scopes, fe, err := m.normalizeScopes(r.Context(), in.NASScopes)
	if err != nil {
		m.internalError(w, "validate nas scopes", err)
		return
	}
	if len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	// Row + scope set commit together: a profile that landed without its scopes
	// would authorize its subscribers on every NAS.
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "begin create", err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	p, err := insertProfile(r.Context(), tx, in)
	if err != nil {
		m.internalError(w, "create", err)
		return
	}
	if err := radius.ReplaceScopes(r.Context(), tx, radius.ProfileScopes, p.ID, scopes); err != nil {
		m.internalError(w, "create nas scopes", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "commit create", err)
		return
	}
	p.NASScopes = orEmptyScopes(scopes)
	_ = auth.Audit(r.Context(), "profile.create", "profile", p.ID, nil, p)
	httpapi.JSON(w, http.StatusCreated, p)
}

// normalizeScopes dedupes and validates a requested FR-64 scope set. An error is
// infrastructure failure; the field errors are per-entry.
func (m *Module) normalizeScopes(ctx context.Context, in []radius.NASScope) ([]radius.NASScope, []httpapi.FieldError, error) {
	scopes := radius.DedupeScopes(in)
	bad, err := radius.ValidateScopes(ctx, m.db, scopes)
	if err != nil {
		return nil, nil, err
	}
	fe := make([]httpapi.FieldError, 0, len(bad))
	for i, msg := range bad {
		fe = append(fe, httpapi.FieldError{Field: fmt.Sprintf("nas_scopes.%d", i), Message: msg})
	}
	return scopes, fe, nil
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
	scopes, fe, err := m.normalizeScopes(r.Context(), in.NASScopes)
	if err != nil {
		m.internalError(w, "validate nas scopes", err)
		return
	}
	if len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "begin update", err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	after, err := updateProfile(r.Context(), tx, id, in)
	if err != nil {
		m.internalError(w, "update", err)
		return
	}
	if err := radius.ReplaceScopes(r.Context(), tx, radius.ProfileScopes, id, scopes); err != nil {
		m.internalError(w, "update nas scopes", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "commit update", err)
		return
	}
	after.NASScopes = orEmptyScopes(scopes)
	_ = auth.Audit(r.Context(), "profile.update", "profile", id, before, after)

	// apply=now (default) pushes the change to existing users immediately:
	// invalidate every affected subscriber's cached policy and surface the online
	// ones for a CoA rate refresh. apply=next_renewal only persists the row —
	// existing users pick it up on their next renewal (Phase 3).
	//
	// Exception (C11 / FR-64.4): a NAS-assignment change is invalidated
	// unconditionally. next_renewal defers *plan* attributes (rate, quota,
	// price), which is a billing decision; an assignment is access control, and
	// the loader reads it fresh on the next cold load regardless. Honouring
	// next_renewal for it would not defer anything — it would just leave the
	// cached views stale for up to the 30 s TTL, so the scope would apply to
	// some subscribers and not others for half a minute. That is
	// indistinguishable from a flaky router, so the cache always follows the
	// scope.
	resp := map[string]any{"profile": after}
	switch {
	case r.URL.Query().Get("apply") != "next_renewal":
		online := m.applyNow(r.Context(), id, auth.ScopeFilter(r.Context()))
		resp["applied"] = "now"
		resp["online_affected"] = online
	case nasScopeChanged(before, after):
		m.invalidateProfileViews(r.Context(), id)
		resp["applied"] = "next_renewal"
		resp["nas_scope_applied"] = "now"
		resp["online_affected"] = []onlineRef{}
	default:
		resp["applied"] = "next_renewal"
		resp["online_affected"] = []onlineRef{}
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

// invalidateProfileViews drops B's cached AuthView for every subscriber on the
// profile (contract C11): the profile carries policy they inherit, so a change
// to it must not be served from a stale view.
func (m *Module) invalidateProfileViews(ctx context.Context, profileID string) {
	ids, err := subscribersOnProfile(ctx, m.db, profileID)
	if err != nil {
		m.log.Error("profiles: load subscribers for invalidation", "error", err, "profile", profileID)
		return
	}
	if err := radius.InvalidatePolicyByProfile(ids); err != nil {
		m.log.Warn("profiles: invalidate policy failed", "error", err, "profile", profileID)
	}
}

// applyNow invalidates B's policy cache for every subscriber on the profile and
// returns the online sessions (scoped) so E can offer a CoA rate refresh.
func (m *Module) applyNow(ctx context.Context, profileID string, scope *auth.ManagerScope) []onlineRef {
	m.invalidateProfileViews(ctx, profileID)
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
