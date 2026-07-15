package importer

// HTTP surface (contract C3): upload -> map -> dry-run -> execute.

import (
	"encoding/base64"
	"encoding/csv"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

const maxUploadBytes = 32 << 20 // 32MB — comfortably above a 10k-row SAS4 export

// uploadRequest is the C3 upload body. The file travels as base64 JSON
// rather than multipart/form-data: the router's frozen enforceJSON
// middleware (contract C2, httpapi/router.go) 415s any POST whose
// Content-Type isn't application/json, and that chain is shared/frozen —
// not this package's to amend. Base64-in-JSON is the compliant shape.
type uploadRequest struct {
	Filename      string `json:"filename"`
	ContentBase64 string `json:"content_base64"`
	Preset        string `json:"preset"`
}

// uploadHandler serves POST /api/v1/import/subscribers. Detects encoding/
// delimiter, stores the decoded UTF-8 text + header, and — when a known
// preset name is given — pre-fills column_map.
func (m *Module) uploadHandler(w http.ResponseWriter, r *http.Request) {
	var in uploadRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.ContentBase64) == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "content_base64", Message: "a base64-encoded CSV file is required"})
		return
	}
	raw, err := base64.StdEncoding.DecodeString(in.ContentBase64)
	if err != nil || len(raw) > maxUploadBytes {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "content_base64", Message: "could not decode base64, or file too large"})
		return
	}

	encoding := detectEncoding(raw)
	text, err := decodeToUTF8(raw, encoding)
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "content_base64", Message: "could not decode file text"})
		return
	}
	delim := detectDelimiter(firstLine(text))

	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = delim
	cr.FieldsPerRecord = -1
	headerRow, err := cr.Read()
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "content_base64", Message: "could not read a header row"})
		return
	}
	for i := range headerRow {
		headerRow[i] = strings.TrimSpace(headerRow[i])
	}

	mgr, _ := auth.ManagerFrom(r.Context())
	filename := in.Filename
	if filename == "" {
		filename = "upload.csv"
	}
	id, err := insertBatch(r.Context(), m.db, filename, encoding, delim, []byte(text), headerRow, managerID(mgr))
	if err != nil {
		m.internalError(w, "upload", err)
		return
	}

	preset := strings.TrimSpace(in.Preset)
	resp := map[string]any{"batch_id": id, "header": headerRow, "encoding": encoding, "status": "uploaded"}
	if preset != "" {
		if colMap := resolvePreset(preset, headerRow); colMap != nil {
			if err := updateColumnMap(r.Context(), m.db, id, colMap, preset); err != nil {
				m.internalError(w, "upload preset map", err)
				return
			}
			resp["column_map"] = colMap
			resp["status"] = "mapped"
		}
	}
	httpapi.JSON(w, http.StatusCreated, resp)
}

type mapRequest struct {
	ColumnMap map[string]string `json:"column_map"`
	Preset    string            `json:"preset"`
}

// mapHandler serves POST /api/v1/import/{batch}/map. A preset seeds the map;
// explicit column_map entries override it field-by-field, so the operator can
// start from "sas4" and fix only what's different.
func (m *Module) mapHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "batch")
	b, err := getBatch(r.Context(), m.db, id)
	if isNotFoundErr(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "import batch not found")
		return
	}
	if err != nil {
		m.internalError(w, "map load", err)
		return
	}
	var in mapRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	colMap := map[string]string{}
	if in.Preset != "" {
		if preset := resolvePreset(in.Preset, b.Header); preset != nil {
			for k, v := range preset {
				colMap[k] = v
			}
		}
	}
	var fe []httpapi.FieldError
	for field, src := range in.ColumnMap {
		if !hikradFields[field] {
			fe = append(fe, httpapi.FieldError{Field: "column_map." + field, Message: "unknown hikrad field"})
			continue
		}
		colMap[field] = src
	}
	if colMap["username"] == "" {
		fe = append(fe, httpapi.FieldError{Field: "column_map.username", Message: "username must be mapped"})
	}
	if len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	if err := updateColumnMap(r.Context(), m.db, id, colMap, in.Preset); err != nil {
		m.internalError(w, "map save", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"batch_id": id, "column_map": colMap, "status": "mapped"})
}

// rowReport is the C3-shaped per-row dry-run entry.
type rowReport struct {
	Row      int               `json:"row"`
	Fields   map[string]string `json:"fields"`
	Errors   []string          `json:"errors"`
	Warnings []string          `json:"warnings"`
	Action   string            `json:"action"`
}

// dryRunHandler serves POST /api/v1/import/{batch}/dry-run: validates every
// row, writes zero subscribers, and returns the per-row report synchronously
// (bounded work — one pass over the file plus two batched lookup queries, no
// per-row round trips, so 10k rows stays well inside the request budget).
func (m *Module) dryRunHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "batch")
	b, err := getBatch(r.Context(), m.db, id)
	if isNotFoundErr(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "import batch not found")
		return
	}
	if err != nil {
		m.internalError(w, "dry-run load", err)
		return
	}
	if len(b.ColumnMap) == 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "not_mapped", "map columns before running a dry-run")
		return
	}
	rows, err := dryRun(r.Context(), m.db, b)
	if err != nil {
		m.internalError(w, "dry-run", err)
		return
	}
	out := make([]rowReport, len(rows))
	created, skipped := 0, 0
	for i, rr := range rows {
		out[i] = rowReport{Row: rr.RowNumber, Fields: rr.Fields, Errors: rr.Errors, Warnings: rr.Warnings, Action: rr.Action}
		if rr.Action == "create" {
			created++
		} else {
			skipped++
		}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"batch_id": id, "rows": out, "total": len(out), "will_create": created, "will_skip": skipped,
	})
}

// executeHandler serves POST /api/v1/import/{batch}/execute: starts (or, on
// an idempotent re-run, resumes) the async create job and returns 202 with a
// progress handle pollable via GET /api/v1/import/{batch}.
func (m *Module) executeHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "batch")
	b, err := getBatch(r.Context(), m.db, id)
	if isNotFoundErr(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "import batch not found")
		return
	}
	if err != nil {
		m.internalError(w, "execute load", err)
		return
	}
	if b.Status != "dry_run" && b.Status != "completed" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "not_dry_run", "run dry-run before executing")
		return
	}
	if prog, ok := m.jobs.get(id); ok && prog.snapshot()["status"] == "running" {
		httpapi.Error(w, http.StatusConflict, "already_running", "this batch is already executing")
		return
	}
	authHeader := r.Header.Get("Authorization")
	prog := m.jobs.start(id, b.RowCount)
	_ = setBatchStatus(r.Context(), m.db, id, "executing")
	go m.runExecute(id, authHeader, prog)

	httpapi.JSON(w, http.StatusAccepted, prog.snapshot())
}

// batchStatusHandler serves GET /api/v1/import/{batch}: batch state + summary,
// merged with live progress while executing. ?rows=1 also returns the last
// dry-run/execute per-row report (for a wizard page reload).
func (m *Module) batchStatusHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "batch")
	b, err := getBatch(r.Context(), m.db, id)
	if isNotFoundErr(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "import batch not found")
		return
	}
	if err != nil {
		m.internalError(w, "status load", err)
		return
	}
	resp := map[string]any{
		"batch_id": id, "status": b.Status, "filename": b.Filename, "encoding": b.Encoding,
		"header": b.Header, "column_map": b.ColumnMap, "preset": b.Preset,
		"row_count": b.RowCount, "summary": b.Summary,
	}
	if prog, ok := m.jobs.get(id); ok {
		resp["progress"] = prog.snapshot()
	}
	if r.URL.Query().Get("rows") == "1" {
		rows, err := listRows(r.Context(), m.db, id)
		if err != nil {
			m.internalError(w, "status rows", err)
			return
		}
		out := make([]map[string]any, len(rows))
		for i, rr := range rows {
			out[i] = map[string]any{
				"row": rr.RowNumber, "fields": rr.Fields, "errors": rr.Errors, "warnings": rr.Warnings,
				"action": rr.Action, "status": rr.Status, "subscriber_id": rr.SubscriberID, "error": rr.Error,
			}
		}
		resp["rows"] = out
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func managerID(mgr *auth.Manager) string {
	if mgr == nil {
		return ""
	}
	return mgr.ID
}
