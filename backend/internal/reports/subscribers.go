package reports

// Subscriber lifecycle reports (C2, FR-46.1). "expiring" delegates to
// ExpiringSubscribers (digest.go) — the single query definition this report
// and C's expiring_digest condition both consume (FR-46.1/AC-46a: one
// definition, not two queries that could drift). "inactive" is the frozen
// sub-PRD 08 §7 definition: an active subscriber with no session whose
// [started_at, stopped_at-or-now) interval overlaps the trailing N-day
// window — "no accounting-visible session", not "no traffic".

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type subscriberRow struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Name      string     `json:"name"`
	Phone     string     `json:"phone"`
	Status    string     `json:"status"`
	ProfileID string     `json:"profile_id"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type profileCountRow struct {
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	Count       int    `json:"count"`
}

const subscriberRowCols = `s.id::text, s.username::text, COALESCE(s.name,''), COALESCE(s.phone,''),
	s.status, COALESCE(s.profile_id::text,''), s.expires_at`

func scanSubscriberRow(rows pgx.Rows) (subscriberRow, error) {
	var sr subscriberRow
	err := rows.Scan(&sr.ID, &sr.Username, &sr.Name, &sr.Phone, &sr.Status, &sr.ProfileID, &sr.ExpiresAt)
	if sr.ExpiresAt != nil {
		u := sr.ExpiresAt.UTC()
		sr.ExpiresAt = &u
	}
	return sr, err
}

func querySubscriberRows(ctx context.Context, db *pgxpool.Pool, q string, args []any) ([]subscriberRow, error) {
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []subscriberRow{}
	for rows.Next() {
		sr, err := scanSubscriberRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

func (m *Module) subscribersReportHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope := auth.ScopeFilter(ctx)
	view := r.URL.Query().Get("view")
	from, to := parseRange(r)
	n := 7
	if v, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && v > 0 {
		n = v
	}

	var (
		rows   []subscriberRow
		byProf []profileCountRow
		err    error
	)

	switch view {
	case "new":
		q := `SELECT ` + subscriberRowCols + ` FROM subscribers s
		       WHERE s.created_at >= $1 AND s.created_at < $2`
		args := []any{from, to}
		if scope != nil {
			q += ` AND s.owner_manager_id = $3::uuid`
			args = append(args, scope.ManagerID)
		}
		q += ` ORDER BY s.created_at DESC`
		rows, err = querySubscriberRows(ctx, m.db, q, args)
	case "expired":
		q := `SELECT ` + subscriberRowCols + ` FROM subscribers s
		       WHERE s.status = 'expired' AND s.expires_at >= $1 AND s.expires_at < $2`
		args := []any{from, to}
		if scope != nil {
			q += ` AND s.owner_manager_id = $3::uuid`
			args = append(args, scope.ManagerID)
		}
		q += ` ORDER BY s.expires_at DESC`
		rows, err = querySubscriberRows(ctx, m.db, q, args)
	case "expiring":
		rows, err = ExpiringSubscribers(ctx, m.db, n, scope)
	case "inactive":
		q := `SELECT ` + subscriberRowCols + ` FROM subscribers s
		       WHERE s.status = 'active' AND NOT EXISTS (
		         SELECT 1 FROM sessions se
		          WHERE se.subscriber_id = s.id AND se.started_at < now()
		            AND COALESCE(se.stopped_at, now()) > now() - make_interval(days => $1)
		       )`
		args := []any{n}
		if scope != nil {
			q += ` AND s.owner_manager_id = $2::uuid`
			args = append(args, scope.ManagerID)
		}
		q += ` ORDER BY s.username`
		rows, err = querySubscriberRows(ctx, m.db, q, args)
	case "by_profile":
		q := `SELECT COALESCE(s.profile_id::text,''), COALESCE(p.name,'(none)'), count(*)::int
		        FROM subscribers s LEFT JOIN profiles p ON p.id = s.profile_id
		       WHERE s.status = 'active'`
		var args []any
		if scope != nil {
			q += ` AND s.owner_manager_id = $1::uuid`
			args = append(args, scope.ManagerID)
		}
		q += ` GROUP BY s.profile_id, p.name ORDER BY count(*) DESC`
		var prows pgx.Rows
		prows, err = m.db.Query(ctx, q, args...)
		if err == nil {
			defer prows.Close()
			for prows.Next() {
				var p profileCountRow
				if err = prows.Scan(&p.ProfileID, &p.ProfileName, &p.Count); err != nil {
					break
				}
				byProf = append(byProf, p)
			}
			if err == nil {
				err = prows.Err()
			}
		}
	default:
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "view", Message: "must be one of: new expired expiring by_profile inactive"})
		return
	}
	if err != nil {
		m.internalError(w, "subscribers report", err)
		return
	}
	if byProf == nil {
		byProf = []profileCountRow{}
	}

	if r.URL.Query().Get("format") == "csv" {
		if !requireExport(w, r) {
			return
		}
		if view == "by_profile" {
			recs := make([][]string, len(byProf))
			for i, p := range byProf {
				recs[i] = []string{p.ProfileID, p.ProfileName, itoa(p.Count)}
			}
			writeCSV(w, "subscribers_"+view+".csv", []string{"profile_id", "profile_name", "count"}, recs)
			return
		}
		recs := make([][]string, len(rows))
		for i, sr := range rows {
			exp := ""
			if sr.ExpiresAt != nil {
				exp = sr.ExpiresAt.Format(time.RFC3339)
			}
			recs[i] = []string{sr.ID, sr.Username, sr.Name, sr.Phone, sr.Status, sr.ProfileID, exp}
		}
		writeCSV(w, "subscribers_"+view+".csv",
			[]string{"id", "username", "name", "phone", "status", "profile_id", "expires_at"}, recs)
		return
	}

	if view == "by_profile" {
		httpapi.JSON(w, http.StatusOK, map[string]any{"rows": byProf})
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"rows": rows, "total": len(rows)})
}
