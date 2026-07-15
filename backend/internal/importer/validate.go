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
	"strconv"
	"strings"
	"time"

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
// (replacing any prior run's rows). Returns the rows in file order.
func dryRun(ctx context.Context, db *pgxpool.Pool, b batch) ([]importRow, error) {
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

	if err := replaceRows(ctx, db, b.ID, results); err != nil {
		return nil, err
	}
	if err := markDryRun(ctx, db, b.ID, len(results)); err != nil {
		return nil, err
	}
	return results, nil
}
