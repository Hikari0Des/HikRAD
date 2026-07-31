package profiles

// Bulk actions on the plan catalogue (FR-111.2). Structurally this mirrors
// subscribers/bulk.go — an explicit id selection replaces the filter, mutations
// run as an async job with per-row failures, each affected row is audited and
// invalidates the RADIUS policy cache for that plan's subscribers — with two
// deliberate divergences, both called out where they occur:
//
//   1. NO manager scoping. Profiles are instance-global; no profile endpoint has
//      ever been scope-filtered, so this one does not invent scoping it would be
//      the only reader of (contrast subscribers, scoped on top of the filter).
//   2. `delete` re-runs FR-7.4's in-use guard per row and refuses a sold plan as
//      a per-row failure, exactly as the single-row delete handler does — a bulk
//      verb must never become a way to delete a plan that was already sold.
//
// The bulk-job registry is copied rather than shared with subscribers: the two
// packages don't cross-import, and the pattern is a few dozen lines.

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

// bulkRequest is the FR-111.2 body. ProfileIDs, when non-empty, is an EXPLICIT
// selection and REPLACES the filter — same semantics as the subscriber
// endpoint's subscriber_ids.
type bulkRequest struct {
	Filter     bulkFilter     `json:"filter"`
	ProfileIDs []string       `json:"profile_ids"`
	Action     string         `json:"action"`
	Params     map[string]any `json:"params"`
}

type bulkFilter struct {
	Q              string `json:"q"`
	Archived       string `json:"archived"` // ""|any = both; "yes"; "no"
	QuotaMode      string `json:"quota_mode"`
	ExpiryBehavior string `json:"expiry_behavior"`
	PoolID         string `json:"pool_id"`
}

type idName struct {
	ID   string
	Name string
}

// --- in-memory job registry (mirrors subscribers/bulk.go) ------------------

type bulkFailure struct {
	ProfileID string `json:"profile_id"`
	Name      string `json:"name"`
	Error     string `json:"error"`
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
	"archive": true, "unarchive": true, "change_pool": true,
	"set_address_list": true, "set_expiry_behavior": true,
	"set_quota_behavior": true, "delete": true,
}

// bulkActionPerm is the permission an action needs — the SAME permission its
// single-row equivalent enforces. delete destroys rows and therefore needs
// profiles.delete; everything else is profiles.edit. A bulk verb must never be
// a route around the permission the one-at-a-time path requires.
func bulkActionPerm(action string) string {
	if action == "delete" {
		return "profiles.delete"
	}
	return "profiles.edit"
}

func (m *Module) bulkHandler(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())

	if req.Action == "export" {
		if mgr == nil || !mgr.Can(auth.PermExport) {
			httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have permission to export")
			return
		}
		m.bulkExport(w, r, req.Filter, req.ProfileIDs)
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

	// Validate params up front so an invalid request fails synchronously (no job
	// starts) — this is where set_quota_behavior:throttle is refused.
	apply, fe := m.buildBulkApply(r.Context(), req.Action, req.Params)
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	targets, err := m.selectTargets(r.Context(), req.Filter, req.ProfileIDs)
	if err != nil {
		m.internalError(w, "bulk select", err)
		return
	}

	job := &bulkJob{
		ID: newJobID(), Action: req.Action, Status: "running",
		Total: len(targets), Failures: []bulkFailure{}, StartedAt: time.Now(),
	}
	m.jobs.add(job)

	actorID, ip, ua := mgr.ID, mgr.IP, mgr.UA
	go m.runBulk(job, targets, apply, actorID, ip, ua)

	httpapi.JSON(w, http.StatusAccepted, job.snapshot())
}

func (m *Module) bulkStatusHandler(w http.ResponseWriter, r *http.Request) {
	j, ok := m.jobs.get(chi.URLParam(r, "id"))
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	httpapi.JSON(w, http.StatusOK, j.snapshot())
}

// bulkApply is a prepared per-row mutation: the SQL SET fragment + args and the
// audit action name. del makes the row DELETE (in-use-guarded) instead.
type bulkApply struct {
	action string // audit action, e.g. profile.bulk_archive
	set    string // SQL after "SET " (ignored when del is true)
	args   []any  // positional args starting at $2 ($1 is the row id)
	del    bool
}

func (m *Module) buildBulkApply(ctx context.Context, action string, params map[string]any) (bulkApply, []httpapi.FieldError) {
	bad := func(f, msg string) []httpapi.FieldError {
		return []httpapi.FieldError{{Field: f, Message: msg}}
	}
	switch action {
	case "archive":
		return bulkApply{action: "profile.bulk_archive", set: "archived = true"}, nil
	case "unarchive":
		return bulkApply{action: "profile.bulk_unarchive", set: "archived = false"}, nil
	case "change_pool":
		pid, _ := params["pool_id"].(string)
		if pid == "" {
			// Explicit "no pool" — clear the reference.
			return bulkApply{action: "profile.bulk_change_pool", set: "pool_id = NULL"}, nil
		}
		var exists bool
		err := m.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ip_pools WHERE id = $1::uuid)`, pid).Scan(&exists)
		if err != nil || !exists {
			return bulkApply{}, bad("params.pool_id", "unknown pool")
		}
		return bulkApply{action: "profile.bulk_change_pool", set: "pool_id = $2::uuid", args: []any{pid}}, nil
	case "set_address_list":
		al, _ := params["address_list"].(string)
		return bulkApply{action: "profile.bulk_set_address_list",
			set: "address_list = NULLIF($2,'')", args: []any{al}}, nil
	case "set_expiry_behavior":
		v, _ := params["expiry_behavior"].(string)
		if v != "block" && v != "expired_pool" {
			return bulkApply{}, bad("params.expiry_behavior", "must be one of: block expired_pool")
		}
		return bulkApply{action: "profile.bulk_set_expiry_behavior",
			set: "expiry_behavior = $2", args: []any{v}}, nil
	case "set_quota_behavior":
		v, _ := params["quota_behavior"].(string)
		switch v {
		case "block", "expired_pool":
			return bulkApply{action: "profile.bulk_set_quota_behavior",
				set: "quota_behavior = $2", args: []any{v}}, nil
		case "throttle":
			// throttle requires a per-plan throttle_rate the single-row validator
			// enforces and a bulk verb can't supply — refuse it rather than
			// manufacture rows the normal path would reject.
			return bulkApply{}, bad("params.quota_behavior", "throttle cannot be set in bulk (it needs a per-plan throttle rate); set it on each plan")
		default:
			return bulkApply{}, bad("params.quota_behavior", "must be one of: block expired_pool")
		}
	case "delete":
		return bulkApply{action: "profile.bulk_delete", del: true}, nil
	}
	return bulkApply{}, bad("action", "unknown bulk action")
}

func (m *Module) runBulk(job *bulkJob, targets []idName, apply bulkApply, actorID, ip, ua string) {
	for _, t := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var err error
		if apply.del {
			// Per-row, not up front: one sold plan in a selection of many must be
			// reported and skipped, not abort the rest (FR-7.4).
			var inUse bool
			if inUse, err = profileInUse(ctx, m.db, t.ID); err == nil {
				if inUse {
					err = errProfileInUse
				} else {
					_, err = m.db.Exec(ctx, `DELETE FROM profiles WHERE id = $1::uuid`, t.ID)
				}
			}
		} else {
			args := append([]any{t.ID}, apply.args...)
			_, err = m.db.Exec(ctx, `UPDATE profiles SET `+apply.set+` WHERE id = $1::uuid`, args...)
		}
		cancel()

		job.mu.Lock()
		job.Done++
		if err != nil {
			job.Failed++
			job.Failures = append(job.Failures, bulkFailure{ProfileID: t.ID, Name: t.Name, Error: friendlyBulkError(err)})
			job.mu.Unlock()
			continue
		}
		job.Succeeded++
		job.mu.Unlock()

		// One audit entry per affected plan + policy invalidation for its
		// subscribers: the plan carries policy they inherit, so a bulk edit that
		// skipped invalidation would serve stale policy for up to the cache TTL.
		actx, acancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = auth.AuditActor(actx, actorID, ip, ua, apply.action, "profile", t.ID, nil, map[string]string{"name": t.Name})
		if !apply.del {
			if ids, ierr := subscribersOnProfile(actx, m.db, t.ID); ierr == nil {
				_ = radius.InvalidatePolicyByProfile(ids)
			}
		}
		acancel()
	}
	job.mu.Lock()
	job.Status = "completed"
	job.mu.Unlock()
}

var errProfileInUse = errors.New("profile in use")

// friendlyBulkError turns a bulk-row error into an operator-actionable string.
func friendlyBulkError(err error) string {
	if errors.Is(err, errProfileInUse) {
		return "in use by subscribers, vouchers or payment history — archive it instead so it stays off the pickers without rewriting what was already sold"
	}
	return err.Error()
}

// selectTargets runs the filter (or the explicit id list) and returns every
// matching plan's id + name. NO scope clause — profiles are instance-global
// (the deliberate divergence from subscribers/bulk.go's selectTargets).
func (m *Module) selectTargets(ctx context.Context, f bulkFilter, ids []string) ([]idName, error) {
	sql := `SELECT id::text, name FROM profiles WHERE 1=1`
	var args []any
	n := 1
	push := func(cond string, v any) {
		sql += fmt.Sprintf(cond, n)
		args = append(args, v)
		n++
	}
	if len(ids) > 0 {
		push(" AND id = ANY($%d::uuid[])", ids)
		sql += " ORDER BY name, id"
		return m.queryTargets(ctx, sql, args)
	}
	if f.Q != "" {
		push(" AND name ILIKE '%%'||$%d||'%%'", f.Q)
	}
	switch f.Archived {
	case "yes":
		sql += " AND archived = true"
	case "no":
		sql += " AND archived = false"
	}
	if f.QuotaMode != "" {
		push(" AND quota_mode = $%d", f.QuotaMode)
	}
	if f.ExpiryBehavior != "" {
		push(" AND expiry_behavior = $%d", f.ExpiryBehavior)
	}
	if f.PoolID != "" {
		push(" AND pool_id = $%d::uuid", f.PoolID)
	}
	sql += " ORDER BY name, id"
	return m.queryTargets(ctx, sql, args)
}

func (m *Module) queryTargets(ctx context.Context, sql string, args []any) ([]idName, error) {
	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []idName
	for rows.Next() {
		var t idName
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// bulkExport streams the selected-or-filtered plans as CSV (export-gated).
func (m *Module) bulkExport(w http.ResponseWriter, r *http.Request, f bulkFilter, ids []string) {
	targets, err := m.selectTargets(r.Context(), f, ids)
	if err != nil {
		m.internalError(w, "export select", err)
		return
	}
	targetIDs := make([]string, len(targets))
	for i, t := range targets {
		targetIDs[i] = t.ID
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT name, price, currency, duration_days, rate_down_kbps, rate_up_kbps,
		        quota_mode, expiry_behavior, quota_behavior,
		        COALESCE((SELECT name FROM ip_pools WHERE id = p.pool_id),''),
		        COALESCE(address_list,''), archived
		   FROM profiles p WHERE id = ANY($1::uuid[]) ORDER BY name`, targetIDs)
	if err != nil {
		m.internalError(w, "export query", err)
		return
	}
	defer rows.Close()

	_ = auth.Audit(r.Context(), "profile.bulk_export", "profile", "", nil, map[string]int{"count": len(targets)})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="profiles.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "price", "currency", "duration_days", "rate_down_kbps", "rate_up_kbps",
		"quota_mode", "expiry_behavior", "quota_behavior", "pool", "address_list", "archived"})
	for rows.Next() {
		var (
			name, currency, quotaMode, expiryBeh, quotaBeh, pool, addrList string
			price                                                          int64
			durationDays, rateDown, rateUp                                 int
			archived                                                       bool
		)
		if err := rows.Scan(&name, &price, &currency, &durationDays, &rateDown, &rateUp,
			&quotaMode, &expiryBeh, &quotaBeh, &pool, &addrList, &archived); err != nil {
			m.log.Error("profiles: export scan", "error", err)
			break
		}
		_ = cw.Write([]string{
			name, strconv.FormatInt(price, 10), currency, strconv.Itoa(durationDays),
			strconv.Itoa(rateDown), strconv.Itoa(rateUp), quotaMode, expiryBeh, quotaBeh,
			pool, addrList, strconv.FormatBool(archived),
		})
	}
	cw.Flush()
}

func newJobID() string {
	return "pbulk-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}
