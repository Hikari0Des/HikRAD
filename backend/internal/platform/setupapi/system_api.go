package setupapi

// GET /api/v1/system/version (FR-52.4, sub-PRD 01 §4): app version + schema
// version + update channel, read by the panel's Settings > System screen and
// by `hikrad update`'s pre-flight check.

import (
	"net/http"
	"os"

	"github.com/hikrad/hikrad/internal/httpapi"
)

// appVersion is set at build time via -ldflags
// "-X github.com/hikrad/hikrad/internal/platform/setupapi.appVersion=v1.0.0"
// (deploy/docker/api.Dockerfile); "dev" for local/unreleased builds.
var appVersion = "dev"

type versionResponse struct {
	AppVersion    string `json:"app_version"`
	SchemaVersion int64  `json:"schema_version"`
	SchemaDirty   bool   `json:"schema_dirty"`
	Channel       string `json:"channel"`
}

func systemVersionHandler(w http.ResponseWriter, r *http.Request) {
	var version int64
	var dirty bool
	// schema_migrations is golang-migrate's own bookkeeping table (created on
	// first Migrate call); a missing row (never migrated) is reported as 0,
	// not an error — the version endpoint must never be the reason a health
	// check fails.
	_ = svc.db.QueryRow(r.Context(), `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)

	channel := os.Getenv("HIKRAD_UPDATE_CHANNEL")
	if channel == "" {
		channel = "stable"
	}

	httpapi.JSON(w, http.StatusOK, versionResponse{
		AppVersion:    appVersion,
		SchemaVersion: version,
		SchemaDirty:   dirty,
		Channel:       channel,
	})
}
