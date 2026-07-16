package portalapi

// GET/PUT /portal/me + PUT /portal/language (contract C2 FR-41.2/41.3, FR-44,
// FR-43). Decision 21 is absolute here: this file must never add a quota
// total/remaining/ceiling field to meResponse — only what the subscriber has
// consumed. Quota enforcement itself stays entirely server-side elsewhere
// (internal/accounting); the portal never even receives the ceiling to check
// client-side.

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live"
	"github.com/hikrad/hikrad/internal/subscribers"
)

type usageView struct {
	UsedDown  int64 `json:"used_down"`
	UsedUp    int64 `json:"used_up"`
	UsedTotal int64 `json:"used_total"`
}

type speedView struct {
	ProfileDown int  `json:"profile_down"`
	ProfileUp   int  `json:"profile_up"`
	LiveDown    *int `json:"live_down,omitempty"`
	LiveUp      *int `json:"live_up,omitempty"`
}

type meResponse struct {
	Status      string     `json:"status"`
	OnlineNow   bool       `json:"online_now"`
	ExpiresAt   *time.Time `json:"expires_at"`
	DaysLeft    int        `json:"days_left"`
	Usage       usageView  `json:"usage"`
	Speed       speedView  `json:"speed"`
	ProfileName string     `json:"profile_name"`
}

// composeMe is the single read backing both GET /portal/me and the response
// PUT /portal/me returns after a self-update.
func (m *Module) composeMe(ctx context.Context, subscriberID string) (meResponse, error) {
	p, err := subscribers.LoadPortalProfile(ctx, m.db, subscriberID)
	if err != nil {
		return meResponse{}, err
	}

	onlineNow := live.Count(subscriberID, "") > 0
	var liveDown, liveUp *int
	if onlineNow {
		sessions, err := live.List(ctx, live.Filter{}, nil)
		if err == nil {
			for _, s := range sessions {
				if s.SubscriberID != subscriberID {
					continue
				}
				d, u := int(s.RateDownBps/1000), int(s.RateUpBps/1000) // bps -> kbps, matching the profile speed unit
				liveDown, liveUp = &d, &u
				break
			}
		}
	}

	usedDown, usedUp := m.consumedSinceCycle(ctx, subscriberID, p.QuotaCycleAnchor)

	daysLeft := 0
	if p.ExpiresAt != nil {
		daysLeft = int(math.Ceil(time.Until(*p.ExpiresAt).Hours() / 24))
		if daysLeft < 0 {
			daysLeft = 0
		}
	}

	return meResponse{
		Status: p.Status, OnlineNow: onlineNow, ExpiresAt: p.ExpiresAt, DaysLeft: daysLeft,
		Usage:       usageView{UsedDown: usedDown, UsedUp: usedUp, UsedTotal: usedDown + usedUp},
		Speed:       speedView{ProfileDown: p.ProfileDownKbps, ProfileUp: p.ProfileUpKbps, LiveDown: liveDown, LiveUp: liveUp},
		ProfileName: p.ProfileName,
	}, nil
}

// consumedSinceCycle sums usage_points since the subscriber's quota cycle
// anchor (same source C's quota evaluator reads — internal/accounting/quota.go
// — but this file never reads the quota ceiling, only the consumed sum).
func (m *Module) consumedSinceCycle(ctx context.Context, subscriberID string, anchor *time.Time) (down, up int64) {
	since := time.Unix(0, 0)
	if anchor != nil {
		since = *anchor
	}
	_ = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(delta_down),0), COALESCE(sum(delta_up),0)
		   FROM usage_points WHERE subscriber_id = $1::uuid AND service <> 'hotspot' AND time >= $2`,
		subscriberID, since).Scan(&down, &up)
	return down, up
}

func (m *Module) meHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	resp, err := m.composeMe(r.Context(), sub.ID)
	if err != nil {
		m.internalError(w, "compose me", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

type updateMeRequest struct {
	Name     *string `json:"name,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	Password *string `json:"password,omitempty" audit:"secret"`
}

// updateMeHandler is FR-44: subscriber-safe fields only (phone; never
// profile/expiry/MAC/status), audit-logged, password change re-encrypts +
// invalidates policy + rotates portal tokens (theft mitigation).
func (m *Module) updateMeHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	ctx := r.Context()
	var in updateMeRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}

	changed := map[string]any{}
	if in.Name != nil {
		if err := subscribers.SetName(ctx, m.db, sub.ID, *in.Name); err != nil {
			m.internalError(w, "update name", err)
			return
		}
		changed["name"] = *in.Name
	}
	if in.Phone != nil {
		if err := subscribers.SetPhone(ctx, m.db, sub.ID, *in.Phone); err != nil {
			m.internalError(w, "update phone", err)
			return
		}
		changed["phone"] = *in.Phone
	}
	passwordChanged := false
	if in.Password != nil && *in.Password != "" {
		if err := subscribers.SetPassword(ctx, m.db, sub.ID, *in.Password); err != nil {
			m.internalError(w, "update password", err)
			return
		}
		passwordChanged = true
		changed["password"] = "[REDACTED]"
	}

	if len(changed) > 0 {
		_ = auth.AuditActor(ctx, sub.ID, sub.IP, sub.UA, "portal.self_update", "subscriber", sub.ID, nil, changed)
	}

	// Theft mitigation (task edge case): a password change revokes every
	// portal refresh session immediately. F's PUT /me response carries no new
	// tokens (matches the already-written client), so the current tab keeps
	// working on its short-lived access token (≤ accessTTL) and then silently
	// requires a fresh login on its next refresh — every other device is cut
	// off right away.
	if passwordChanged {
		if err := revokeAllSessions(ctx, m.db, sub.ID); err != nil {
			m.log.Warn("portalapi: revoke sessions after password change failed", "error", err)
		}
	}

	resp, err := m.composeMe(ctx, sub.ID)
	if err != nil {
		m.internalError(w, "compose me", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

type languageRequest struct {
	Language string `json:"language" validate:"required,oneof=en ar ku"`
}

func (m *Module) languageHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	var in languageRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if err := subscribers.SetLanguage(r.Context(), m.db, sub.ID, in.Language); err != nil {
		m.internalError(w, "set language", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
