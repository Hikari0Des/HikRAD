package monitorsvc

// Run is the hikrad-monitor process entrypoint: it starts the probe engine, the
// periodic condition evaluator, and the self-check samplers, all sharing one DB
// and Redis. It runs until ctx is cancelled. The HTTP read/CRUD surface is NOT
// started here — that's served by hikrad-api via the registered Module.

import (
	"context"
	"log/slog"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// conditionInterval is how often the non-probe conditions (disk/backlog/reject/
// balance/digest) are evaluated. Well inside the ≤ 60 s delivery budget (FR-36).
const conditionInterval = 60 * time.Second

// Run wires and drives every monitor loop until ctx ends.
func Run(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) error {
	alerts := newAlertEngine(db, rdb, settings, log)
	engine := NewEngine(db, rdb, alerts, log)
	cond := newConditions(db, rdb, settings, alerts, log)
	sc := newSelfCheck(db, rdb, log)
	subEvents := newSubscriberEvents(db, rdb, settings, log)

	go engine.Run(ctx)
	go sc.runOnlineSampler(ctx)
	go sc.runHealthProbes(ctx)
	go runConditionLoop(ctx, cond, log)
	go subEvents.run(ctx)

	log.Info("hikrad-monitor loops started")
	<-ctx.Done()
	log.Info("hikrad-monitor shutting down")
	return nil
}

func runConditionLoop(ctx context.Context, cond *conditions, log *slog.Logger) {
	t := time.NewTicker(conditionInterval)
	defer t.Stop()
	// Evaluate once shortly after boot so a standing condition (e.g. disk already
	// full) alerts without waiting a full interval.
	cond.evaluate(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cond.evaluate(ctx)
		}
	}
}
