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

// runExecute processes every eligible row of the batch, self-dispatching
// creates through router with authHeader forwarded (the executing manager's
// bearer token, captured before this goroutine detached from the request —
// see the "5-minute access token" note in status-agent-3.md: a batch whose
// wall-clock runtime exceeds the token TTL will see its remaining rows stay
// 'pending' and complete on a subsequent execute call with a fresh token,
// which is safe because this whole path is idempotent).
func (m *Module) runExecute(batchID, authHeader string, prog *execProgress) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

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

		body := createBody{Username: r.Fields["username"], Password: r.Fields["password"]}
		if v := r.Fields["name"]; v != "" {
			body.Name = &v
		}
		if v := r.Fields["phone"]; v != "" {
			body.Phone = &v
		}
		if v := r.Fields["email"]; v != "" {
			body.Email = &v
		}
		if v := r.Fields["address"]; v != "" {
			body.Address = &v
		}
		if v := r.Fields["profile_id"]; v != "" {
			body.ProfileID = &v
		}
		if v := r.Fields["expires_at"]; v != "" {
			body.ExpiresAt = &v
		}
		if v := r.Fields["service_type"]; v != "" {
			body.ServiceType = &v
		}

		buf, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/subscribers", bytes.NewReader(buf))
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
