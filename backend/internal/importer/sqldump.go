package importer

// MySQL dump → CSV conversion (v4-5, FR-138): Radius Manager and Super Speed
// Radius both export as a `mysqldump`-style .sql file. Rather than a fourth
// import path, this converts the dump's rows for one table into the SAME
// header+CSV shape a plain upload already produces, so the existing
// map/dry-run/execute pipeline (encoding.go/execute.go/validate.go) never
// needs to know the file didn't start as a spreadsheet.
//
// This is NOT a SQL parser: it recognizes exactly the one statement shape
// every mysqldump produces — `INSERT INTO \`table\` (\`col\`, ...) VALUES
// (v1, v2, ...), (v1, v2, ...), ...;` — and extracts values from the FIRST
// table name it finds an INSERT for (a dump normally has one CREATE/INSERT
// pair per exported table; when several exist, the caller names which one to
// use). Real Radius Manager / Super Speed Radius schemas were not available
// to verify column names against (the same documented uncertainty the SAS4
// CSV preset already carries) — the presets below are a best-effort starting
// guess an operator confirms/adjusts in the mapping step, never a hard
// contract.

import (
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var errNoInsertFound = errors.New("importer: no INSERT INTO statement found in this dump")

// insertStmtRE matches one `INSERT INTO \`table\` (...) VALUES ...;`
// statement, case-insensitively, tolerating an optional "INTO" backtick and
// either single- or double-quoted table names (some tools emit those).
var insertStmtRE = regexp.MustCompile(`(?is)INSERT\s+INTO\s+[` + "`" + `"']?(\w+)[` + "`" + `"']?\s*\(([^)]*)\)\s*VALUES\s*(.+?);`)

// dumpTableRow is one extracted row: the column names (from the INSERT's own
// column list — a mysqldump always states them explicitly) and that row's
// values in the same order.
type dumpToCSVResult struct {
	Header []string
	Rows   [][]string
}

// mysqlDumpToCSV extracts every row inserted into the FIRST table the dump
// names (or, when table is non-empty, only that table's INSERTs — a dump can
// contain several tables, and the caller knows which one holds subscriber or
// plan data for this legacy product).
func mysqlDumpToCSV(dump string, table string) (dumpToCSVResult, error) {
	matches := insertStmtRE.FindAllStringSubmatch(dump, -1)
	if len(matches) == 0 {
		return dumpToCSVResult{}, errNoInsertFound
	}

	var header []string
	var rows [][]string
	for _, m := range matches {
		gotTable, colsRaw, valuesRaw := m[1], m[2], m[3]
		if table != "" && !strings.EqualFold(gotTable, table) {
			continue
		}
		cols := splitAndTrim(colsRaw, ',')
		for i := range cols {
			cols[i] = strings.Trim(cols[i], "`\"' ")
		}
		if header == nil {
			header = cols
		} else if len(cols) != len(header) {
			// A dump occasionally repeats INSERT INTO for the same table with
			// a differently-ordered column list (rare, but tolerated rather
			// than silently misaligning columns) — skip a mismatched batch's
			// rows rather than guess.
			continue
		}
		tuples, err := splitValueTuples(valuesRaw)
		if err != nil {
			return dumpToCSVResult{}, fmt.Errorf("parsing VALUES for table %s: %w", gotTable, err)
		}
		for _, tuple := range tuples {
			vals, err := parseSQLTuple(tuple)
			if err != nil {
				return dumpToCSVResult{}, fmt.Errorf("parsing a value tuple for table %s: %w", gotTable, err)
			}
			if len(vals) != len(header) {
				continue // malformed/truncated tuple — skip rather than misalign
			}
			rows = append(rows, vals)
		}
	}
	if header == nil {
		return dumpToCSVResult{}, errNoInsertFound
	}
	return dumpToCSVResult{Header: header, Rows: rows}, nil
}

// splitAndTrim splits on sep, trimming surrounding whitespace from each part
// (used only for the column-name list, which never contains the separator
// inside a quoted value the way row data can).
func splitAndTrim(s string, sep rune) []string {
	parts := strings.Split(s, string(sep))
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

// splitValueTuples splits a VALUES clause's `(...), (...), (...)` list into
// its individual `(...)` tuples, respecting quoted strings and escaped
// quotes so a comma or paren inside a value never splits the tuple early.
func splitValueTuples(s string) ([]string, error) {
	var tuples []string
	depth := 0
	inQuote := byte(0)
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == '\\' {
				i++ // skip the escaped character
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
		case c == '\'' || c == '"':
			inQuote = c
		case c == '(':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				if start < 0 {
					return nil, errors.New("unbalanced parentheses in VALUES clause")
				}
				tuples = append(tuples, s[start:i])
				start = -1
			}
		}
	}
	if depth != 0 {
		return nil, errors.New("unbalanced parentheses in VALUES clause")
	}
	return tuples, nil
}

// parseSQLTuple splits one tuple's comma-separated values, unquoting string
// literals (single or double quoted, backslash-escaped) and passing numeric/
// NULL literals through as plain text — CSV cells are always text, and the
// downstream validators (validate.go) already parse numbers/dates from text
// exactly as they would from a real spreadsheet cell.
func parseSQLTuple(tuple string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(tuple); i++ {
		c := tuple[i]
		switch {
		case inQuote != 0:
			if c == '\\' && i+1 < len(tuple) {
				i++
				cur.WriteByte(unescapeSQLChar(tuple[i]))
				continue
			}
			if c == inQuote {
				// A doubled quote ('' or "") inside a quoted string is the
				// SQL-standard escape for a literal quote character.
				if i+1 < len(tuple) && tuple[i+1] == inQuote {
					cur.WriteByte(inQuote)
					i++
					continue
				}
				inQuote = 0
				continue
			}
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			inQuote = c
		case c == ',':
			out = append(out, normalizeSQLLiteral(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if inQuote != 0 {
		return nil, errors.New("unterminated string literal")
	}
	out = append(out, normalizeSQLLiteral(cur.String()))
	return out, nil
}

func unescapeSQLChar(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	default:
		return c
	}
}

// normalizeSQLLiteral trims a bare (unquoted) literal and maps SQL NULL to an
// empty CSV cell — every other unquoted token (numbers, dates already
// quoted above) passes through as-is.
func normalizeSQLLiteral(s string) string {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "NULL") {
		return ""
	}
	return s
}

// writeCSV renders a header+rows result as CSV text (comma-delimited, RFC
// 4180 quoting via encoding/csv) — the canonical shape upload() stores for
// every source format.
func writeCSV(res dumpToCSVResult) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	if err := w.Write(res.Header); err != nil {
		return "", err
	}
	for _, row := range res.Rows {
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return sb.String(), nil
}
