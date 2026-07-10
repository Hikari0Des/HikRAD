package subscribers

// Expiry sweep (FR-1.2, sub-PRD 04 §7). Auth-time is the authority for whether a
// subscriber is expired; this job only aligns the persisted status column so the
// panel lists and reports agree, within one cycle (≤ 5 min). It must not flap a
// user renewed mid-sweep: the UPDATE re-checks expires_at against now() at write
// time (compare against the current row, not a snapshot), so a renewal that
// pushed expires_at into the future is never overwritten to 'expired'.

import (
	"context"
	"time"

	"github.com/hikrad/hikrad/internal/radius"
)

func (m *Module) runSweep(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := m.sweepOnce(ctx); err != nil {
				m.log.Error("subscribers: expiry sweep failed", "error", err)
			} else if n > 0 {
				m.log.Info("subscribers: expiry sweep flipped rows", "count", n)
			}
		}
	}
}

// sweepOnce flips active→expired for rows whose expires_at has passed, and (the
// reverse) expired→active for rows whose expires_at is now in the future (a
// renewal that happened while the column still read 'expired'). Disabled rows
// are never touched — a disabled account stays disabled regardless of expiry.
// Returns the number of rows changed. Invalidates B's policy cache for each.
func (m *Module) sweepOnce(ctx context.Context) (int, error) {
	rows, err := m.db.Query(ctx,
		`UPDATE subscribers
		    SET status = CASE
		        WHEN expires_at IS NOT NULL AND expires_at <= now() THEN 'expired'
		        ELSE 'active' END
		  WHERE status <> 'disabled'
		    AND (
		         (status <> 'expired' AND expires_at IS NOT NULL AND expires_at <= now())
		      OR (status =  'expired' AND (expires_at IS NULL OR expires_at > now()))
		    )
		 RETURNING id::text`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		_ = radius.InvalidatePolicy(id)
	}
	return len(ids), nil
}
