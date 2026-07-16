package subscribers

// Bulk actions on filtered user lists (FR-4, AC-4a). The filter is executed
// server-side (never just the visible page), scoped to the caller (FR-27.2).
// Mutating actions run as an async job with a progress + per-row failure report;
// each affected row gets its own audit entry and policy invalidation. CSV export
// is synchronous and gated by the `export` permission.

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
)

// bulkRequest is the C7-D body: {filter, action, params}, plus subscriber_ids
// (added 2026-07-16, owner-requested).
type bulkRequest struct {
	Filter bulkFilter `json:"filter"`
	// SubscriberIDs, when non-empty, is an EXPLICIT selection and REPLACES the
	// filter entirely — the operator ticked these rows and means these rows.
	//
	// Filter-only bulk was the original contract, and it makes the common case
	// ("disable these three") impossible: you cannot express an arbitrary set of
	// rows as a filter, so an operator either edits them one at a time or builds
	// a filter that happens to match and hopes it matches nothing else. The
	// filter is still the right tool for "every expired subscriber", so both are
	// supported and the selection wins when present.
	//
	// The manager scope still applies on top (selectTargets), so a scoped agent
	// cannot reach another agent's subscriber by pasting its id.
	SubscriberIDs []string       `json:"subscriber_ids"`
	Action        string         `json:"action"`
	Params        map[string]any `json:"params"`
}

type bulkFilter struct {
	Status         string  `json:"status"`
	ProfileID      string  `json:"profile_id"`
	OwnerManagerID string  `json:"owner_manager_id"`
	Q              string  `json:"q"`
	ExpiringBefore *string `json:"expiring_before"` // RFC3339
	// ServiceType (FR-61) mirrors the panel's list filter. It MUST exist here:
	// a bulk action runs against this server-side filter, not the visible rows,
	// so a filter the panel offers but this struct ignores would silently apply
	// the action to every service type instead of the one on screen.
	ServiceType string `json:"service_type"`
}

type idUser struct {
	ID       string
	Username string
}

// --- in-memory job registry ------------------------------------------------
// v1 runs a single hikrad-api process, so an in-memory registry with TTL cleanup
// is sufficient for bulk-job progress. It never persists across a restart — a
// job in flight during a restart is simply lost (the mutation is not, since each
// row commits independently); documented, acceptable for the operator workflow.

type bulkFailure struct {
	SubscriberID string `json:"subscriber_id"`
	Username     string `json:"username"`
	Error        string `json:"error"`
}

type bulkJob struct {
	mu        sync.Mutex
	ID        string        `json:"id"`
	Action    string        `json:"action"`
	Status    string        `json:"status"` // running | completed
	Total     int           `json:"total"`
	Done      int           `json:"done"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Failures  []bulkFailure `json:"failures"`
	StartedAt time.Time     `json:"started_at"`
}

func (j *bulkJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	fails := make([]bulkFailure, len(j.Failures))
	copy(fails, j.Failures)
	return map[string]any{
		"id": j.ID, "action": j.Action, "status": j.Status,
		"total": j.Total, "done": j.Done, "succeeded": j.Succeeded,
		"failed": j.Failed, "failures": fails, "started_at": j.StartedAt.UTC(),
	}
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*bulkJob
}

func newJobRegistry() *jobRegistry { return &jobRegistry{jobs: map[string]*bulkJob{}} }

func (reg *jobRegistry) add(j *bulkJob) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.jobs[j.ID] = j
	// Opportunistic cleanup of jobs older than an hour.
	for id, old := range reg.jobs {
		if time.Since(old.StartedAt) > time.Hour {
			delete(reg.jobs, id)
		}
	}
}

func (reg *jobRegistry) get(id string) (*bulkJob, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	j, ok := reg.jobs[id]
	return j, ok
}

// --- handlers --------------------------------------------------------------

var mutatingBulkActions = map[string]bool{
	"enable": true, "disable": true, "change_profile": true,
	"extend_expiry": true, "move_owner": true, "set_service_type": true,
	"delete": true,
}

// bulkActionPerm is the permission an action needs. Everything is
// subscribers.edit except delete, which destroys rows and therefore requires the
// same subscribers.delete an operator needs to remove one by hand — a bulk verb
// must never be a way around the permission its single-row equivalent enforces.
func bulkActionPerm(action string) string {
	if action == "delete" {
		return "subscribers.delete"
	}
	return "subscribers.edit"
}

func (m *Module) bulkHandler(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	scope := auth.ScopeFilter(r.Context())

	if req.Action == "export" {
		if mgr == nil || !mgr.Can(auth.PermExport) {
			httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have permission to export")
			return
		}
		m.bulkExport(w, r, req.Filter, req.SubscriberIDs, scope)
		return
	}
	if !mutatingBulkActions[req.Action] {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "action", Message: "unknown bulk action"})
		return
	}
	if perm := bulkActionPerm(req.Action); mgr == nil || !mgr.Can(perm) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have permission to "+perm)
		return
	}

	// Validate action params up front so an invalid request fails synchronously.
	apply, fe := m.buildBulkApply(r.Context(), req.Action, req.Params)
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	targets, err := m.selectTargets(r.Context(), req.Filter, req.SubscriberIDs, scope)
	if err != nil {
		m.internalError(w, "bulk select", err)
		return
	}

	job := &bulkJob{
		ID: newJobID(), Action: req.Action, Status: "running",
		Total: len(targets), Failures: []bulkFailure{}, StartedAt: time.Now(),
	}
	m.jobs.add(job)

	// Capture the actor for detached audit writes (the request context is done
	// once the goroutine runs; AuditActor takes an explicit actor).
	actorID, ip, ua := mgr.ID, mgr.IP, mgr.UA
	go m.runBulk(job, targets, apply, actorID, ip, ua)

	httpapi.JSON(w, http.StatusAccepted, job.snapshot())
}

// bulkStatusHandler serves GET /api/v1/subscribers/bulk/{id}.
func (m *Module) bulkStatusHandler(w http.ResponseWriter, r *http.Request) {
	j, ok := m.jobs.get(chi.URLParam(r, "id"))
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	httpapi.JSON(w, http.StatusOK, j.snapshot())
}

// bulkApply is a prepared per-row mutation: the SQL SET fragment + args and the
// audit action name.
type bulkApply struct {
	action string // audit action, e.g. subscriber.bulk_disable
	set    string // SQL after "SET " (ignored when del is true)
	args   []any  // positional args starting at $2 ($1 is the row id)
	// del makes the row DELETE rather than UPDATE. The one action here that
	// destroys data, so it is a distinct field rather than a magic `set` string
	// — nothing should be able to become a delete by accident.
	del bool
}

func (m *Module) buildBulkApply(ctx context.Context, action string, params map[string]any) (bulkApply, []httpapi.FieldError) {
	bad := func(f, msg string) []httpapi.FieldError {
		return []httpapi.FieldError{{Field: f, Message: msg}}
	}
	switch action {
	case "enable":
		return bulkApply{action: "subscriber.bulk_enable", set: "status='active', disabled_reason=NULL"}, nil
	case "disable":
		reason, _ := params["disabled_reason"].(string)
		return bulkApply{action: "subscriber.bulk_disable",
			set: "status='disabled', disabled_reason=$2", args: []any{nilStr(reason)}}, nil
	case "change_profile":
		pid, _ := params["profile_id"].(string)
		if pid == "" {
			return bulkApply{}, bad("params.profile_id", "required for change_profile")
		}
		// Skip archived target profiles (edge case): reject the whole request so
		// the operator picks an active plan.
		var archived bool
		err := m.db.QueryRow(ctx, `SELECT archived FROM profiles WHERE id = $1::uuid`, pid).Scan(&archived)
		if err != nil {
			return bulkApply{}, bad("params.profile_id", "unknown profile")
		}
		if archived {
			return bulkApply{}, bad("params.profile_id", "cannot bulk-assign an archived profile")
		}
		return bulkApply{action: "subscriber.bulk_change_profile",
			set: "profile_id=$2::uuid", args: []any{pid}}, nil
	case "extend_expiry":
		days, ok := toInt(params["days"])
		if !ok || days == 0 {
			return bulkApply{}, bad("params.days", "a non-zero integer is required")
		}
		return bulkApply{action: "subscriber.bulk_extend_expiry",
			set:  "expires_at = COALESCE(expires_at, now()) + ($2 || ' days')::interval",
			args: []any{strconv.Itoa(days)}}, nil
	case "move_owner":
		owner, _ := params["owner_manager_id"].(string)
		if owner == "" {
			return bulkApply{}, bad("params.owner_manager_id", "required for move_owner")
		}
		return bulkApply{action: "subscriber.bulk_move_owner",
			set: "owner_manager_id=$2::uuid", args: []any{owner}}, nil
	case "set_service_type":
		v, _ := params["service_type"].(string)
		switch v {
		case "pppoe", "hotspot", "dual":
		default:
			return bulkApply{}, bad("params.service_type", "must be one of: pppoe hotspot dual")
		}
		return bulkApply{action: "subscriber.bulk_set_service_type",
			set: "service_type=$2", args: []any{v}}, nil
	case "delete":
		return bulkApply{action: "subscriber.bulk_delete", del: true}, nil
	}
	return bulkApply{}, bad("action", "unknown bulk action")
}

func (m *Module) runBulk(job *bulkJob, targets []idUser, apply bulkApply, actorID, ip, ua string) {
	for _, t := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var err error
		if apply.del {
			// Per-row, not up front: one protected subscriber in a selection of
			// fifty must be reported and skipped, not abort the other 49.
			var paid bool
			if paid, err = hasFinancialHistory(ctx, m.db, t.ID); err == nil {
				if paid {
					err = errHasFinancialHistory
				} else {
					_, err = m.db.Exec(ctx, `DELETE FROM subscribers WHERE id = $1::uuid`, t.ID)
				}
			}
		} else {
			args := append([]any{t.ID}, apply.args...)
			_, err = m.db.Exec(ctx, `UPDATE subscribers SET `+apply.set+` WHERE id = $1::uuid`, args...)
		}
		cancel()

		job.mu.Lock()
		job.Done++
		if err != nil {
			job.Failed++
			job.Failures = append(job.Failures, bulkFailure{SubscriberID: t.ID, Username: t.Username, Error: friendlyBulkError(err)})
			job.mu.Unlock()
			continue
		}
		job.Succeeded++
		job.mu.Unlock()

		// One audit entry per affected row (AC-4a) + policy invalidation. The
		// audit is the ONLY trace a deleted subscriber leaves, so it records the
		// username rather than just the id that no longer resolves to anything.
		actx, acancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = auth.AuditActor(actx, actorID, ip, ua, apply.action, "subscriber", t.ID, nil, map[string]string{"username": t.Username})
		acancel()
		_ = radius.InvalidatePolicy(t.ID)
	}
	job.mu.Lock()
	job.Status = "completed"
	job.mu.Unlock()
}

// friendlyBulkError turns the errors a bulk row can hit into something an
// operator can act on. A raw pgx FK message in the failures list tells them
// nothing about what to do next.
func friendlyBulkError(err error) string {
	if errors.Is(err, errHasFinancialHistory) {
		return "has billing history — disable instead of deleting, so their payments stay attached to them"
	}
	return err.Error()
}

// selectTargets runs the filter server-side and returns every matching row's id
// and username (scoped).
func (m *Module) selectTargets(ctx context.Context, f bulkFilter, ids []string, scope *auth.ManagerScope) ([]idUser, error) {
	sql := `SELECT id::text, username::text FROM subscribers WHERE 1=1`
	var args []any
	n := 1
	push := func(cond string, v any) {
		sql += fmt.Sprintf(cond, n)
		args = append(args, v)
		n++
	}
	// An explicit selection replaces the filter — but NOT the scope clause
	// below, which is appended either way. A scoped agent listing another
	// agent's id simply matches no row.
	if len(ids) > 0 {
		push(" AND id = ANY($%d::uuid[])", ids)
		if scope != nil {
			push(" AND owner_manager_id = $%d::uuid", scope.ManagerID)
		}
		sql += " ORDER BY id"
		return m.queryTargets(ctx, sql, args)
	}
	if f.Status != "" {
		push(" AND status = $%d", f.Status)
	}
	if f.ProfileID != "" {
		push(" AND profile_id = $%d::uuid", f.ProfileID)
	}
	if f.OwnerManagerID != "" {
		push(" AND owner_manager_id = $%d::uuid", f.OwnerManagerID)
	}
	if f.ServiceType != "" {
		push(" AND service_type = $%d", f.ServiceType)
	}
	if f.Q != "" {
		sql += fmt.Sprintf(" AND (subscriber_fold(username::text) ILIKE '%%'||subscriber_fold($%d)||'%%' OR subscriber_fold(name) ILIKE '%%'||subscriber_fold($%d)||'%%')", n, n)
		args = append(args, f.Q)
		n++
	}
	if f.ExpiringBefore != nil && *f.ExpiringBefore != "" {
		push(" AND expires_at < $%d", *f.ExpiringBefore)
	}
	if scope != nil {
		push(" AND owner_manager_id = $%d::uuid", scope.ManagerID)
	}
	sql += " ORDER BY id"
	return m.queryTargets(ctx, sql, args)
}

func (m *Module) queryTargets(ctx context.Context, sql string, args []any) ([]idUser, error) {
	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []idUser
	for rows.Next() {
		var t idUser
		if err := rows.Scan(&t.ID, &t.Username); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// bulkExport streams the selected-or-filtered subscribers as CSV (scoped,
// export-gated). It takes ids for the same reason every other action does: an
// operator who ticked rows and pressed Export means those rows.
func (m *Module) bulkExport(w http.ResponseWriter, r *http.Request, f bulkFilter, ids []string, scope *auth.ManagerScope) {
	targets, err := m.selectTargets(r.Context(), f, ids, scope)
	if err != nil {
		m.internalError(w, "export select", err)
		return
	}
	targetIDs := make([]string, len(targets))
	for i, t := range targets {
		targetIDs[i] = t.ID
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT username::text, COALESCE(name,''), COALESCE(phone,''), status,
		        COALESCE((SELECT name FROM profiles WHERE id = s.profile_id),''),
		        COALESCE(to_char(expires_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),''),
		        COALESCE(host(static_ip),'')
		   FROM subscribers s WHERE id = ANY($1::uuid[]) ORDER BY username`, targetIDs)
	if err != nil {
		m.internalError(w, "export query", err)
		return
	}
	defer rows.Close()

	// Audit the export as a single action (FR-4 / FR-28): who exported how many.
	_ = auth.Audit(r.Context(), "subscriber.bulk_export", "subscriber", "", nil, map[string]int{"count": len(targets)})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="subscribers.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"username", "name", "phone", "status", "profile", "expires_at", "static_ip"})
	for rows.Next() {
		rec := make([]string, 7)
		if err := rows.Scan(&rec[0], &rec[1], &rec[2], &rec[3], &rec[4], &rec[5], &rec[6]); err != nil {
			m.log.Error("subscribers: export scan", "error", err)
			break
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
}

func newJobID() string {
	return "bulk-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func nilStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	}
	return 0, false
}
