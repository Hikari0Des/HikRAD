package reports

import (
	"net/http"
	"strconv"
	"strings"
)

// csvField quotes a field containing a comma, quote or newline (RFC 4180) —
// mirrors billing/ledger_api.go's helper so exports look identical across
// modules.
func csvField(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// writeCSV streams a header + rows as a downloadable CSV response.
func writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	quoted := make([]string, len(header))
	for i, h := range header {
		quoted[i] = csvField(h)
	}
	_, _ = w.Write([]byte(strings.Join(quoted, ",") + "\n"))
	for _, rec := range rows {
		out := make([]string, len(rec))
		for i, f := range rec {
			out[i] = csvField(f)
		}
		_, _ = w.Write([]byte(strings.Join(out, ",") + "\n"))
	}
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
func itoa(n int) string     { return strconv.Itoa(n) }
