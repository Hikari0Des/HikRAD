package importer

// Execute (FR-6): runs dry-run's "create" rows (plus any still-"pending" row
// from an interrupted prior run) through the REAL subscribers API,
// self-dispatched in-process onto the same chi router every module mounts
// on — so validation, audit logging and B's policy-cache invalidation are
// exactly what a manual create would produce, never re-implemented here.
// Idempotent by construction: a row already status='imported' is skipped
// outright, and the create endpoint's own 409 on a duplicate username
// (someone else imported/created it between dry-run and execute, or this is
// a second execute of the same batch) is treated as a normal skip, not a
// failure — this is also how the "dry-run → data changed before execute"
// edge case self-heals without extra bookkeeping.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

type execProgress struct {
	mu                                    sync.Mutex
	Total, Done, Created, Skipped, Failed int
	Status                                string // running | completed
}

func (p *execProgress) snapshot() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{
		"status": p.Status, "total": p.Total, "done": p.Done,
		"created": p.Created, "skipped": p.Skipped, "failed": p.Failed,
	}
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*execProgress
}

func newJobRegistry() *jobRegistry { return &jobRegistry{jobs: map[string]*execProgress{}} }

func (reg *jobRegistry) start(batchID string, total int) *execProgress {
	p := &execProgress{Total: total, Status: "running"}
	reg.mu.Lock()
	reg.jobs[batchID] = p
	reg.mu.Unlock()
	return p
}

func (reg *jobRegistry) get(batchID string) (*execProgress, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	p, ok := reg.jobs[batchID]
	return p, ok
}

// createBody is the subset of the subscribers writeInput this wizard fills.
type createBody struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	Name        *string `json:"name,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Email       *string `json:"email,omitempty"`
	Address     *string `json:"address,omitempty"`
	ProfileID   *string `json:"profile_id,omitempty"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
	ServiceType *string `json:"service_type,omitempty"`
}

// targetPath is the real create endpoint an entity's rows self-dispatch to.
func targetPath(entity string) string {
	if entity == "profiles" {
		return "/api/v1/profiles"
	}
	return "/api/v1/subscribers"
}

// buildBody marshals one dry-run row's mapped fields into the create-endpoint
// body for its entity.
func buildBody(entity string, f map[string]string) []byte {
	if entity == "profiles" {
		return buildProfileBody(f)
	}
	body := createBody{Username: f["username"], Password: f["password"]}
	if v := f["name"]; v != "" {
		body.Name = &v
	}
	if v := f["phone"]; v != "" {
		body.Phone = &v
	}
	if v := f["email"]; v != "" {
		body.Email = &v
	}
	if v := f["address"]; v != "" {
		body.Address = &v
	}
	if v := f["profile_id"]; v != "" {
		body.ProfileID = &v
	}
	if v := f["expires_at"]; v != "" {
		body.ExpiresAt = &v
	}
	if v := f["service_type"]; v != "" {
		body.ServiceType = &v
	}
	buf, _ := json.Marshal(body)
	return buf
}

// buildProfileBody maps a dry-run plan row onto the profiles create body. Rates
// come from the `*_kbps` fields dry-run computed via parseRateKbpsLocal; the
// profiles API re-validates everything authoritatively. Unset numeric/enum
// fields are omitted so the API applies its own defaults (unlimited quota,
// block behaviors, session_limit 1, currency IQD).
func buildProfileBody(f map[string]string) []byte {
	body := map[string]any{"name": f["name"]}
	putInt := func(field, key string) {
		if v := strings.TrimSpace(f[field]); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				body[key] = n
			}
		}
	}
	putInt("price", "price")
	putInt("duration_days", "duration_days")
	putInt("session_limit", "session_limit_default")
	putInt("rate_down_kbps", "rate_down_kbps")
	putInt("rate_up_kbps", "rate_up_kbps")
	if v := strings.TrimSpace(f["currency"]); v != "" {
		body["currency"] = strings.ToUpper(v)
	}
	if v := strings.TrimSpace(f["quota_mode"]); v != "" {
		body["quota_mode"] = strings.ToLower(v)
	}
	if v := strings.TrimSpace(f["quota_total_bytes"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			body["quota_total_bytes"] = n
		}
	}
	if v := strings.TrimSpace(f["expiry_behavior"]); v != "" {
		body["expiry_behavior"] = strings.ToLower(v)
	}
	if v := strings.TrimSpace(f["quota_behavior"]); v != "" {
		body["quota_behavior"] = strings.ToLower(v)
	}
	if v := strings.TrimSpace(f["address_list"]); v != "" {
		body["address_list"] = v
	}
	buf, _ := json.Marshal(body)
	return buf
}

// runExecute processes every eligible row of the batch, self-dispatching
// creates through router with authHeader forwarded (the executing manager's
// bearer token, captured before this goroutine detached from the request —
// see the "5-minute access token" note in status-agent-3.md: a batch whose
// wall-clock runtime exceeds the token TTL will see its remaining rows stay
// 'pending' and complete on a subsequent execute call with a fresh token,
// which is safe because this whole path is idempotent).
func (m *Module) runExecute(batchID, entity, authHeader string, prog *execProgress) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	path := targetPath(entity)
	rows, err := listRows(ctx, m.db, batchID)
	if err != nil {
		m.log.Error("importer: execute list rows", "batch", batchID, "error", err)
		prog.mu.Lock()
		prog.Status = "completed"
		prog.mu.Unlock()
		return
	}

	created, skipped, failed, alreadyImported := 0, 0, 0, 0
	tokenExpired := false
	for _, r := range rows {
		if r.Status == "imported" {
			// Idempotent re-run: already created on a prior execute of this
			// batch — not re-dispatched, and not counted as a fresh create
			// (a re-run summary must read "0 created" when nothing new
			// happened, or an operator re-running execute would think it
			// just imported the whole file again).
			alreadyImported++
			prog.mu.Lock()
			prog.Done++
			prog.Created++
			prog.mu.Unlock()
			continue
		}
		if tokenExpired {
			break // leave remaining rows 'pending' for a re-run with a fresh token
		}
		if r.Action != "create" && !hasAlreadyExistsWarning(r) {
			_ = markRowResult(ctx, m.db, r.ID, "skipped", "", "")
			skipped++
			prog.mu.Lock()
			prog.Done++
			prog.Skipped++
			prog.mu.Unlock()
			continue
		}

		// Profiles have no unique-name constraint, so the create endpoint never
		// 409s a duplicate — check here immediately before dispatch and skip, the
		// way the subscriber path leans on the username 409. Checked per row (not
		// batched at dry-run) so a name created earlier in THIS execute is also
		// caught (idempotent re-run + within-run safety).
		if entity == "profiles" {
			if name := r.Fields["name"]; name != "" {
				var exists bool
				_ = m.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM profiles WHERE lower(name) = lower($1))`, name).Scan(&exists)
				if exists {
					_ = markRowResult(ctx, m.db, r.ID, "skipped", "", "a plan with this name already exists")
					skipped++
					prog.mu.Lock()
					prog.Done++
					prog.Skipped++
					prog.mu.Unlock()
					continue
				}
			}
		}

		buf := buildBody(entity, r.Fields)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authHeader)
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)

		switch rec.Code {
		case http.StatusCreated:
			var created201 struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &created201)
			_ = markRowResult(ctx, m.db, r.ID, "imported", created201.ID, "")
			created++
		case http.StatusConflict:
			_ = markRowResult(ctx, m.db, r.ID, "skipped", "", "username already exists")
			skipped++
		case http.StatusUnauthorized:
			tokenExpired = true
			continue // row stays 'pending'
		default:
			_ = markRowResult(ctx, m.db, r.ID, "failed", "", extractErrorMessage(rec.Body.Bytes(), rec.Code))
			failed++
		}
		prog.mu.Lock()
		prog.Done++
		if rec.Code == http.StatusCreated {
			prog.Created++
		} else if rec.Code == http.StatusConflict {
			prog.Skipped++
		} else if rec.Code != http.StatusUnauthorized {
			prog.Failed++
		}
		prog.mu.Unlock()
	}

	summary := map[string]any{
		"created": created, "skipped": skipped, "failed": failed, "already_imported": alreadyImported,
	}
	if tokenExpired {
		summary["note"] = "stopped early: authorization expired mid-batch; re-run execute to finish the remaining rows"
	}
	_ = markExecuted(ctx, m.db, batchID, summary)
	prog.mu.Lock()
	prog.Status = "completed"
	prog.mu.Unlock()
}

func hasAlreadyExistsWarning(r importRow) bool {
	for _, w := range r.Warnings {
		if w == "username already exists — execute will skip this row" {
			return true
		}
	}
	return false
}

func extractErrorMessage(body []byte, status int) string {
	var env struct {
		Error struct {
			Message     string `json:"message"`
			FieldErrors []struct {
				Field   string `json:"field"`
				Message string `json:"message"`
			} `json:"field_errors"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Error.Message == "" {
		return http.StatusText(status)
	}
	msg := env.Error.Message
	for _, fe := range env.Error.FieldErrors {
		msg += "; " + fe.Field + ": " + fe.Message
	}
	return msg
}
