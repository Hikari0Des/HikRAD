package subscribers

// User-detail composition (FR-3, AC-3a). The single screen Sara lives on. This
// endpoint returns everything the panel needs to render the page header without
// further round-trips: the subscriber, a profile summary, the owner, a live
// online flag (C6), override badges, and the links/ids to lazily fetch usage
// (C7-C), session history (C7-C) and the audit trail (A's endpoint).

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live"
)

type profileSummary struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	RateDownKbps   int     `json:"rate_down_kbps"`
	RateUpKbps     int     `json:"rate_up_kbps"`
	DurationDays   int     `json:"duration_days"`
	QuotaMode      string  `json:"quota_mode"`
	ExpiryBehavior string  `json:"expiry_behavior"`
	QuotaBehavior  string  `json:"quota_behavior"`
	Archived       bool    `json:"archived"`
	PoolName       *string `json:"pool_name"`
}

type ownerSummary struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type overrideBadges struct {
	Rate         bool `json:"rate"`
	Price        bool `json:"price"`
	SessionLimit bool `json:"session_limit"`
	StaticIP     bool `json:"static_ip"`
}

type detailResponse struct {
	Subscriber Subscriber        `json:"subscriber"`
	Profile    *profileSummary   `json:"profile"`
	Owner      *ownerSummary     `json:"owner"`
	Live       liveFlag          `json:"live"`
	Overrides  overrideBadges    `json:"overrides"`
	Links      map[string]string `json:"links"`
}

type liveFlag struct {
	Online   bool `json:"online"`
	Sessions int  `json:"sessions"`
}

// detailHandler serves GET /api/v1/subscribers/{id} as the composition contract.
func (m *Module) detailHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := getByID(r.Context(), m.db, id, auth.ScopeFilter(r.Context()))
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}
	if err != nil {
		m.internalError(w, "detail get", err)
		return
	}

	resp := detailResponse{
		Subscriber: s,
		Overrides: overrideBadges{
			Rate:         s.RateOverride != nil && *s.RateOverride != "",
			Price:        s.PriceOverride != nil,
			SessionLimit: s.SessionLimitOverride != nil,
			StaticIP:     s.StaticIP != nil && *s.StaticIP != "",
		},
		Links: map[string]string{
			"usage":    "/api/v1/usage/subscriber/" + id,
			"sessions": "/api/v1/sessions?subscriber_id=" + id,
			"audit":    "/api/v1/audit-log?entity_type=subscriber&entity_id=" + id,
		},
	}
	if s.ProfileID != nil {
		if ps, err := m.loadProfileSummary(r.Context(), *s.ProfileID); err == nil {
			resp.Profile = ps
		} else {
			m.log.Warn("subscribers: detail profile summary", "error", err, "profile", *s.ProfileID)
		}
	}
	if s.OwnerManagerID != nil {
		if os, err := m.loadOwner(r.Context(), *s.OwnerManagerID); err == nil {
			resp.Owner = os
		}
	}
	n := live.Count(s.ID, "")
	resp.Live = liveFlag{Online: n > 0, Sessions: n}

	httpapi.JSON(w, http.StatusOK, resp)
}

func (m *Module) loadProfileSummary(ctx context.Context, profileID string) (*profileSummary, error) {
	var ps profileSummary
	err := m.db.QueryRow(ctx,
		`SELECT p.id::text, p.name, p.rate_down_kbps, p.rate_up_kbps, p.duration_days,
		        p.quota_mode, p.expiry_behavior, p.quota_behavior, p.archived,
		        (SELECT name FROM ip_pools WHERE id = p.pool_id)
		   FROM profiles p WHERE p.id = $1::uuid`, profileID).Scan(
		&ps.ID, &ps.Name, &ps.RateDownKbps, &ps.RateUpKbps, &ps.DurationDays,
		&ps.QuotaMode, &ps.ExpiryBehavior, &ps.QuotaBehavior, &ps.Archived, &ps.PoolName)
	if err != nil {
		return nil, err
	}
	return &ps, nil
}

func (m *Module) loadOwner(ctx context.Context, managerID string) (*ownerSummary, error) {
	var o ownerSummary
	err := m.db.QueryRow(ctx,
		`SELECT id::text, username FROM managers WHERE id = $1::uuid`, managerID).Scan(&o.ID, &o.Username)
	if err != nil {
		return nil, err
	}
	return &o, nil
}
