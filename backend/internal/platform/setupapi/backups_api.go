package setupapi

// GET /api/v1/backups (FR-51): read-only history for the panel's Settings >
// System screen ("last backup age" per sub-PRD 01 §5). scripts/hikrad writes
// backup_runs rows directly via `psql` (it runs on the host, stopping/
// restarting services around a restore — that can't happen from inside the
// API container it would be restarting), so this handler only reads.

import (
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

type backupRunView struct {
	ID            int64      `json:"id"`
	Filename      string     `json:"filename"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	SizeBytes     *int64     `json:"size_bytes,omitempty"`
	SchemaVersion *int64     `json:"schema_version,omitempty"`
	Encrypted     bool       `json:"encrypted"`
	Status        string     `json:"status"`
	Error         *string    `json:"error,omitempty"`
	Trigger       string     `json:"trigger"`
}

func listBackupsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := svc.db.Query(r.Context(),
		`SELECT id, filename, started_at, finished_at, size_bytes, schema_version, encrypted, status, error, trigger
		   FROM backup_runs ORDER BY started_at DESC LIMIT 50`)
	if err != nil {
		svc.log.Error("list backups failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	defer rows.Close()

	items := []backupRunView{}
	for rows.Next() {
		var v backupRunView
		if err := rows.Scan(&v.ID, &v.Filename, &v.StartedAt, &v.FinishedAt, &v.SizeBytes,
			&v.SchemaVersion, &v.Encrypted, &v.Status, &v.Error, &v.Trigger); err != nil {
			svc.log.Error("scan backup row failed", "error", err)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		items = append(items, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}
