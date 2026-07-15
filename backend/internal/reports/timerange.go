package reports

import (
	"net/http"
	"time"
)

// epoch is the default "from" when the caller omits one — reports read
// existing data only, so an unbounded lower bound is safe and keeps an
// omitted ?from from turning into a confusing 400 (edge case: a report
// requested on a fresh, near-empty install must still return a friendly
// empty shape rather than an error).
var epoch = time.Unix(0, 0).UTC()

// parseRange reads ?from&to (RFC3339), defaulting to [epoch, now]. A bad
// value is silently ignored in favor of the default rather than 400ing —
// reports are read-only, so leniency here costs nothing.
func parseRange(r *http.Request) (from, to time.Time) {
	from, to = epoch, time.Now().UTC()
	q := r.URL.Query()
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t.UTC()
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t.UTC()
		}
	}
	return from, to
}
