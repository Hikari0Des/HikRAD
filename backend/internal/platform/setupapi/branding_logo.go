package setupapi

// Logo upload/delete (v2 phase 11, FR-91, contract C3): the disk-backed
// half of Settings > Branding. POST validates + stores via
// platform.StoreLogo (size/type/dimension checks, magic-byte sniffing —
// never the client's declared Content-Type/filename) and writes the
// resulting served path to branding.logo_url; DELETE clears both the file
// and the setting. Both are audited exactly like every other settings
// change (FR-53.1).

import (
	"io"
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
)

const maxLogoUploadBytes = 2 << 20 // headroom above platform.StoreLogo's own 1 MiB ceiling for multipart overhead

func uploadBrandingLogoHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxLogoUploadBytes); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "could not parse form")
		return
	}
	file, _, err := r.FormFile("logo")
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "logo", Message: "a file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxLogoUploadBytes+1))
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "could not read uploaded file")
		return
	}

	servedPath, _, err := platform.StoreLogo(data)
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "logo", Message: err.Error()})
		return
	}

	ctx := r.Context()
	if err := svc.settings.Set(ctx, "branding.logo_url", servedPath); err != nil {
		svc.log.Error("settings: set branding.logo_url failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = auth.Audit(ctx, "settings.update", "settings", "branding", nil, map[string]string{"logo_url": "changed"})
	httpapi.JSON(w, http.StatusOK, readGroup(ctx, "branding"))
}

func deleteBrandingLogoHandler(w http.ResponseWriter, r *http.Request) {
	if err := platform.DeleteLogo(); err != nil {
		svc.log.Error("settings: delete logo file failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	ctx := r.Context()
	if err := svc.settings.Set(ctx, "branding.logo_url", ""); err != nil {
		svc.log.Error("settings: clear branding.logo_url failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = auth.Audit(ctx, "settings.update", "settings", "branding", nil, map[string]string{"logo_url": "removed"})
	httpapi.JSON(w, http.StatusOK, readGroup(ctx, "branding"))
}
