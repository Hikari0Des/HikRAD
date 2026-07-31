package importer

// Dry-run validation (FR-6): per-row errors/warnings/action, zero writes.
// Categories covered: missing required fields, duplicate username within the
// file, duplicate username already in the DB (previously imported or
// otherwise created — reported so the operator sees what execute will skip,
// matching FR-6's idempotent re-run), unknown profile (the wizard's "create
// profile" remediation is the existing profiles API + a dry-run re-run, not
// auto-creation here — a guessed price/duration would be worse than asking),
// and unparseable expiry dates.

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

// expiryLayouts are the date formats SAS4-family exports commonly use.
var expiryLayouts = []string{
	time.RFC3339,
	"2006-01-02",
	"2006-01-02 15:04:05",
	"02/01/2006",
	"01/02/2006",
	"2006/01/02",
	"02-01-2006",
}

// parseExpiry tries each known layout; "" is valid (no expiry set).
func parseExpiry(s string) (time.Time, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range expiryLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true, nil
		}
	}
	return time.Time{}, false, errBadExpiry
}

var errBadExpiry = &validationErr{"unrecognized expiry date format"}

type validationErr struct{ msg string }

func (e *validationErr) Error() string { return e.msg }

// parseCSVRows reads b.RawCSV with b.Delimiter into raw records (header
// excluded), mapped through column_map into {hikrad_field: value}.
func parseCSVRows(b batch) ([]map[string]string, error) {
	reader := csv.NewReader(strings.NewReader(string(b.RawCSV)))
	reader.Comma = []rune(b.Delimiter)[0]
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	all, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	header := all[0]
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}

	// Invert column_map to {source_column: hikrad_field}.
	srcToField := make(map[string]string, len(b.ColumnMap))
	for field, src := range b.ColumnMap {
		srcToField[src] = field
	}

	out := make([]map[string]string, 0, len(all)-1)
	for _, rec := range all[1:] {
		row := map[string]string{}
		for src, field := range srcToField {
			if idx, ok := colIdx[src]; ok && idx < len(rec) {
				row[field] = strings.TrimSpace(rec[idx])
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// dryRun validates every row of the batch and persists the per-row report
// (replacing any prior run's rows). Returns the rows in file order. Dispatches
// on the batch entity (FR-111.3) — the subscriber path is unchanged.
func dryRun(ctx context.Context, db *pgxpool.Pool, b batch) ([]importRow, error) {
	if b.Entity == "profiles" {
		return dryRunProfiles(ctx, db, b)
	}
	return dryRunSubscribers(ctx, db, b)
}

func dryRunSubscribers(ctx context.Context, db *pgxpool.Pool, b batch) ([]importRow, error) {
	rawRows, err := parseCSVRows(b)
	if err != nil {
		return nil, err
	}

	profileByName := map[string]string{} // lower(name) -> id
	prows, err := db.Query(ctx, `SELECT id::text, name FROM profiles WHERE archived = false`)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var id, name string
		if err := prows.Scan(&id, &name); err != nil {
			prows.Close()
			return nil, err
		}
		profileByName[strings.ToLower(name)] = id
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, err
	}

	// Existing usernames (case-insensitive) — a single batched query rather
	// than one per row (10k-row budget, task 6).
	lowerUsernames := make([]string, 0, len(rawRows))
	for _, r := range rawRows {
		if u := strings.ToLower(strings.TrimSpace(r["username"])); u != "" {
			lowerUsernames = append(lowerUsernames, u)
		}
	}
	existing := map[string]bool{}
	if len(lowerUsernames) > 0 {
		erows, err := db.Query(ctx,
			`SELECT lower(username::text) FROM subscribers WHERE lower(username::text) = ANY($1::text[])`,
			lowerUsernames)
		if err != nil {
			return nil, err
		}
		for erows.Next() {
			var u string
			if err := erows.Scan(&u); err != nil {
				erows.Close()
				return nil, err
			}
			existing[u] = true
		}
		erows.Close()
		if err := erows.Err(); err != nil {
			return nil, err
		}
	}

	seenInFile := map[string]int{} // lower(username) -> first row_number
	results := make([]importRow, 0, len(rawRows))
	for i, r := range rawRows {
		rowNum := i + 1
		res := importRow{RowNumber: rowNum, Fields: r, Action: "skip"}
		username := strings.TrimSpace(r["username"])
		lowerU := strings.ToLower(username)

		if username == "" {
			res.Errors = append(res.Errors, "username is required")
		}
		if strings.TrimSpace(r["password"]) == "" {
			res.Errors = append(res.Errors, "password is required")
		}
		if p := strings.TrimSpace(r["phone"]); p != "" {
			if _, ok := normalizePhoneLocal(p); !ok {
				res.Errors = append(res.Errors, "phone is not a valid Iraqi mobile number")
			}
		}
		if e := strings.TrimSpace(r["email"]); e != "" {
			if !validateEmailLocal(e) {
				res.Errors = append(res.Errors, "email is not a valid email address")
			}
		}
		if prof := strings.TrimSpace(r["profile"]); prof != "" {
			if id, ok := profileByName[strings.ToLower(prof)]; ok {
				res.Fields["profile_id"] = id
			} else {
				res.Errors = append(res.Errors, "unknown profile: "+prof+" (create it, then re-run dry-run)")
			}
		}
		if exp := strings.TrimSpace(r["expires_at"]); exp != "" {
			if t, ok, err := parseExpiry(exp); err != nil {
				res.Errors = append(res.Errors, err.Error()+": "+exp)
			} else if ok {
				res.Fields["expires_at"] = t.Format(time.RFC3339)
			}
		}
		if st := strings.TrimSpace(r["service_type"]); st != "" {
			if v, ok := normalizeServiceType(st); ok {
				res.Fields["service_type"] = v
			} else {
				res.Errors = append(res.Errors, "service_type must be pppoe, hotspot, dual, or a yes/no hotspot flag: "+st)
			}
		}

		if username != "" {
			if first, dup := seenInFile[lowerU]; dup {
				res.Errors = append(res.Errors, "duplicate username within this file (first seen row "+strconv.Itoa(first)+")")
			} else {
				seenInFile[lowerU] = rowNum
				if existing[lowerU] {
					res.Warnings = append(res.Warnings, "username already exists — execute will skip this row")
				}
			}
		}

		switch {
		case len(res.Errors) > 0:
			res.Action = "skip"
		case existing[lowerU]:
			res.Action = "skip"
		default:
			res.Action = "create"
		}
		results = append(results, res)
	}

	// FR-105 license-cap heads-up: mirror execute's real behavior (creates
	// succeed until cap+buffer, the rest are refused with license_subscriber_cap)
	// so the operator sees the overflow here rather than as a wall of failed
	// rows after execute. Skipped entirely for an unlimited/absent license.
	if platform.CachedLicenseMaxSubscribers() > 0 {
		var current int
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM subscribers`).Scan(&current); err != nil {
			return nil, err
		}
		projected := current
		for i := range results {
			if results[i].Action != "create" {
				continue
			}
			projected++
			if st := platform.EvalSubscriberCap(projected); st.Blocked {
				results[i].Warnings = append(results[i].Warnings,
					fmt.Sprintf("exceeds licensed subscriber cap (%d, +%d buffer) — execute will refuse this row", st.Cap, st.Buffer))
			}
		}
	}

	if err := replaceRows(ctx, db, b.ID, results); err != nil {
		return nil, err
	}
	if err := markDryRun(ctx, db, b.ID, len(results)); err != nil {
		return nil, err
	}
	return results, nil
}

var validQuotaModes = map[string]bool{"unlimited": true, "total": true, "split": true}
var validExpiryBehaviors = map[string]bool{"block": true, "expired_pool": true}
var validQuotaBehaviors = map[string]bool{"block": true, "throttle": true, "expired_pool": true}

// dryRunProfiles validates plan rows (FR-111.3). The structural difference from
// subscribers: profiles.name has no uniqueness constraint, so a duplicate name
// (in-file OR already in the live table) is a SKIP the operator sees here rather
// than a create-time 409 that never comes (the create endpoint accepts a
// duplicate name). Rate columns accept the FR-95 prefix forms.
func dryRunProfiles(ctx context.Context, db *pgxpool.Pool, b batch) ([]importRow, error) {
	rawRows, err := parseCSVRows(b)
	if err != nil {
		return nil, err
	}

	// Existing plan names (case-insensitive) in one batched query.
	lowerNames := make([]string, 0, len(rawRows))
	for _, r := range rawRows {
		if n := strings.ToLower(strings.TrimSpace(r["name"])); n != "" {
			lowerNames = append(lowerNames, n)
		}
	}
	existing := map[string]bool{}
	if len(lowerNames) > 0 {
		erows, err := db.Query(ctx,
			`SELECT lower(name) FROM profiles WHERE lower(name) = ANY($1::text[])`, lowerNames)
		if err != nil {
			return nil, err
		}
		for erows.Next() {
			var n string
			if err := erows.Scan(&n); err != nil {
				erows.Close()
				return nil, err
			}
			existing[n] = true
		}
		erows.Close()
		if err := erows.Err(); err != nil {
			return nil, err
		}
	}

	requireInt := func(res *importRow, field string, required bool) {
		v := strings.TrimSpace(res.Fields[field])
		if v == "" {
			if required {
				res.Errors = append(res.Errors, field+" is required")
			}
			return
		}
		if _, err := strconv.Atoi(v); err != nil {
			res.Errors = append(res.Errors, field+" must be a whole number: "+v)
		}
	}
	requireRate := func(res *importRow, field string) {
		v := strings.TrimSpace(res.Fields[field])
		if v == "" {
			return
		}
		if kbps, ok := parseRateKbpsLocal(v); ok {
			res.Fields[field+"_kbps"] = strconv.Itoa(kbps)
		} else {
			res.Errors = append(res.Errors, field+" is not a valid rate (e.g. 10M, 512k, or a number of kbps): "+v)
		}
	}

	seenInFile := map[string]int{}
	results := make([]importRow, 0, len(rawRows))
	for i, r := range rawRows {
		rowNum := i + 1
		res := importRow{RowNumber: rowNum, Fields: r, Action: "skip"}
		name := strings.TrimSpace(r["name"])
		lowerN := strings.ToLower(name)

		if name == "" {
			res.Errors = append(res.Errors, "name is required")
		}
		requireInt(&res, "price", false)
		requireInt(&res, "duration_days", false)
		requireInt(&res, "session_limit", false)
		requireInt(&res, "quota_total_bytes", false)
		requireRate(&res, "rate_down")
		requireRate(&res, "rate_up")
		if qm := strings.TrimSpace(r["quota_mode"]); qm != "" && !validQuotaModes[strings.ToLower(qm)] {
			res.Errors = append(res.Errors, "quota_mode must be one of: unlimited total split: "+qm)
		}
		if eb := strings.TrimSpace(r["expiry_behavior"]); eb != "" && !validExpiryBehaviors[strings.ToLower(eb)] {
			res.Errors = append(res.Errors, "expiry_behavior must be one of: block expired_pool: "+eb)
		}
		if qb := strings.TrimSpace(r["quota_behavior"]); qb != "" && !validQuotaBehaviors[strings.ToLower(qb)] {
			res.Errors = append(res.Errors, "quota_behavior must be one of: block throttle expired_pool: "+qb)
		}

		if name != "" {
			if first, dup := seenInFile[lowerN]; dup {
				res.Errors = append(res.Errors, "duplicate plan name within this file (first seen row "+strconv.Itoa(first)+")")
			} else {
				seenInFile[lowerN] = rowNum
				if existing[lowerN] {
					res.Warnings = append(res.Warnings, "a plan with this name already exists — execute will skip this row")
				}
			}
		}

		switch {
		case len(res.Errors) > 0:
			res.Action = "skip"
		case existing[lowerN]:
			res.Action = "skip"
		default:
			res.Action = "create"
		}
		results = append(results, res)
	}

	if err := replaceRows(ctx, db, b.ID, results); err != nil {
		return nil, err
	}
	if err := markDryRun(ctx, db, b.ID, len(results)); err != nil {
		return nil, err
	}
	return results, nil
}

// parseRateKbpsLocal mirrors @hikrad/shared's parseRateKbps (FR-95): a number
// with an optional k/m/g suffix, binary multiples (k=1, m=1024, g=1024²), the
// same the RouterOS adapter expects. Empty → (0, true) = unlimited. Duplicated
// rather than imported for the same reason validateEmailLocal is: dry-run only
// needs to know a value WOULD parse; the real create call re-validates.
func parseRateKbpsLocal(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	mult := 1
	last := s[len(s)-1]
	switch last {
	case 'k', 'K':
		mult, s = 1, s[:len(s)-1]
	case 'm', 'M':
		mult, s = 1024, s[:len(s)-1]
	case 'g', 'G':
		mult, s = 1024*1024, s[:len(s)-1]
	}
	s = strings.TrimSpace(s)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return int(f*float64(mult) + 0.5), true
}
