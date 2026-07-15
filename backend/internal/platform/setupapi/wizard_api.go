package setupapi

// First-run wizard backend (FR-49.3, contract C4): GET/POST
// /api/v1/setup/{status,license,admin,branding}, active only while no
// manager exists yet. Once the first admin is created every one of these
// (except status, which stays a harmless read) refuses with setup_complete —
// license changes move to /api/v1/license and branding moves to
// /api/v1/settings/branding, both of which require a real session.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
)

// clientIP mirrors auth.clientIP (unexported there): the first X-Forwarded-For
// hop (Caddy sets it) else the transport remote address, port stripped. Setup
// handlers run pre-auth so they can't call into auth's request parsing.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); first != "" {
			return first
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func adminExists(ctx context.Context) (bool, error) {
	n, err := auth.ManagerCount(ctx, svc.db)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// guardSetupOpen writes setup_complete and returns false when a manager
// already exists; callers return immediately when it returns false.
func guardSetupOpen(w http.ResponseWriter, r *http.Request) bool {
	done, err := adminExists(r.Context())
	if err != nil {
		svc.log.Error("setup: admin-exists check failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return false
	}
	if done {
		httpapi.Error(w, http.StatusForbidden, "setup_complete",
			"initial setup is already complete; use the authenticated panel endpoints instead")
		return false
	}
	return true
}

type setupStatusResponse struct {
	AdminExists      bool `json:"admin_exists"`
	LicenseInstalled bool `json:"license_installed"`
}

func setupStatusHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	done, err := adminExists(ctx)
	if err != nil {
		svc.log.Error("setup: status admin check failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_, installed, err := platform.LoadLicenseRecord(ctx, svc.db)
	if err != nil {
		svc.log.Error("setup: status license check failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, setupStatusResponse{AdminExists: done, LicenseInstalled: installed})
}

func setupGetLicenseHandler(w http.ResponseWriter, r *http.Request) {
	if !guardSetupOpen(w, r) {
		return
	}
	getLicenseHandler(w, r)
}

func setupUploadLicenseHandler(w http.ResponseWriter, r *http.Request) {
	if !guardSetupOpen(w, r) {
		return
	}
	installLicense(w, r, "setup.license_upload")
}

type setupAdminRequest struct {
	Username string `json:"username" validate:"required,min=3,max=64"`
	Password string `json:"password" validate:"required,min=8,max=256"`
}

func setupCreateAdminHandler(w http.ResponseWriter, r *http.Request) {
	if !guardSetupOpen(w, r) {
		return
	}
	var req setupAdminRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	view, err := auth.CreateFirstAdmin(r.Context(), svc.db, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrSetupAlreadyComplete) {
			httpapi.Error(w, http.StatusForbidden, "setup_complete", "initial setup is already complete")
			return
		}
		svc.log.Error("setup: create admin failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = auth.AuditActor(r.Context(), view.ID, clientIP(r), r.UserAgent(), "setup.admin_create", "manager", view.ID, nil, view)
	httpapi.JSON(w, http.StatusCreated, view)
}

func setupGetBrandingHandler(w http.ResponseWriter, r *http.Request) {
	if !guardSetupOpen(w, r) {
		return
	}
	httpapi.JSON(w, http.StatusOK, readBranding(r.Context()))
}

func setupPutBrandingHandler(w http.ResponseWriter, r *http.Request) {
	if !guardSetupOpen(w, r) {
		return
	}
	putSettingsGroupHandler(setupBrandingRequest(w, r))
}

// setupBrandingRequest rewrites the request's chi URL param so the wizard's
// branding step can share putSettingsGroupHandler's field-validation and
// storage instead of duplicating it under a different key shape.
func setupBrandingRequest(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request) {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}
	rctx.URLParams.Add("group", "branding")
	return w, r
}
