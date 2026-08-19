package radius

import "testing"

// TestDecideNack_Matrix covers the hybrid-fallback decision (FR-151) at the
// pure-function level — no DB, no network. The end-to-end version (real
// state transitions through the dispatcher tick loop against a real
// Postgres) lives in coa_outbox_db_test.go.
func TestDecideNack_Matrix(t *testing.T) {
	cases := []struct {
		name         string
		row          OutboxRow
		retryMax     int
		wantRetry    bool
		wantFallback bool
		wantFail     bool
	}{
		{
			name:      "retries while budget remains",
			row:       OutboxRow{ID: "1", CurrentOp: CoAActionUpdate, AttemptCount: 1},
			retryMax:  3,
			wantRetry: true,
		},
		{
			name:         "update exhausts retries, falls back to disconnect",
			row:          OutboxRow{ID: "1", CurrentOp: CoAActionUpdate, AttemptCount: 3},
			retryMax:     3,
			wantFallback: true,
		},
		{
			name:      "already-fallen-back update (now current_op disconnect) still retries under budget",
			row:       OutboxRow{ID: "1", Action: CoAActionUpdate, CurrentOp: CoAActionDisconnect, FallbackUsed: true, AttemptCount: 1},
			retryMax:  3,
			wantRetry: true,
		},
		{
			name:     "disconnect leg exhausts its own retries: terminal failure, no second fallback",
			row:      OutboxRow{ID: "1", Action: CoAActionUpdate, CurrentOp: CoAActionDisconnect, FallbackUsed: true, AttemptCount: 3},
			retryMax: 3,
			wantFail: true,
		},
		{
			name:     "a plain disconnect row (never had an update leg) exhausts retries: terminal failure",
			row:      OutboxRow{ID: "1", Action: CoAActionDisconnect, CurrentOp: CoAActionDisconnect, AttemptCount: 3},
			retryMax: 3,
			wantFail: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := decideNack(c.row, c.retryMax)
			if d.Retry != c.wantRetry || d.Fallback != c.wantFallback || d.Fail != c.wantFail {
				t.Fatalf("decideNack(%+v, %d) = %+v, want retry=%v fallback=%v fail=%v",
					c.row, c.retryMax, d, c.wantRetry, c.wantFallback, c.wantFail)
			}
			if d.Fallback && d.FallbackReason == "" {
				t.Errorf("fallback decision must carry a non-empty reason (gate item 4: visible in the row)")
			}
		})
	}
}

func TestDecideNack_DefaultRetryMaxIsSane(t *testing.T) {
	if defaultCoARetryMax < 1 {
		t.Fatalf("defaultCoARetryMax = %d, must be at least 1", defaultCoARetryMax)
	}
}
