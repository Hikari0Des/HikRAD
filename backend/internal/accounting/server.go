package accounting

// Service wires the hikrad-acct pipeline together: the HTTP ingest (:8082
// /acct), the counters endpoint (/internal/acct/counters), the stream consumer,
// the stale reaper, the spill drainer, and the durable counter mirror. All of it
// runs in one process, which is why the FR-40 counters can be plain in-process
// atomics.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Config parameterizes the accounting Service.
type Config struct {
	// HTTPAddr is the ingest/counters listen address (default ":8082" — the
	// service name+port FreeRADIUS forwards to, contract C6).
	HTTPAddr string
	// SpillDir is the disk-WAL directory (compose mounts a volume at /spill). An
	// empty dir disables spill (unit tests).
	SpillDir string
	// InterimInterval is the expected accounting interim period; the reaper's
	// stale/reap thresholds are multiples of it (FR-38). Default 300 s.
	InterimInterval time.Duration
}

func (c Config) withDefaults() Config {
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8082"
	}
	if c.InterimInterval <= 0 {
		c.InterimInterval = 300 * time.Second
	}
	return c
}

// Service is the assembled accounting pipeline.
type Service struct {
	db              *pgxpool.Pool
	rdb             *redis.Client
	log             *slog.Logger
	spill           *spillFile
	counters        *counters
	nas             *nasResolver
	quota           *quotaEvaluator
	now             func() time.Time
	interimInterval time.Duration
	consumerName    string
	httpAddr        string
}

// New builds the Service. The returned cleanup closes the spill file. db may be
// nil only in unit tests that never touch the consumer/reaper.
func New(ctx context.Context, cfg Config, db *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) (*Service, func(), error) {
	cfg = cfg.withDefaults()
	sp, err := newSpill(cfg.SpillDir)
	if err != nil {
		return nil, nil, err
	}
	c := &counters{}
	if err := loadCountersWithRetry(ctx, c, db, rdb); err != nil {
		// A fresh DB (no row yet) or Postgres still genuinely unreachable
		// after the retry window must not stop boot; the counters simply
		// start from zero and the first flush creates the row.
		log.Warn("acct: load counters failed (starting from zero)", "error", err)
	}
	s := &Service{
		db:              db,
		rdb:             rdb,
		log:             log,
		spill:           sp,
		counters:        c,
		nas:             newNASResolver(db),
		quota:           newQuotaEvaluator(db),
		now:             time.Now,
		interimInterval: cfg.InterimInterval,
		consumerName:    consumerName(),
		httpAddr:        cfg.HTTPAddr,
	}
	cleanup := func() { _ = sp.close() }
	return s, cleanup, nil
}

// consumerName is deliberately a fixed identity, not hostname+PID. hikrad-acct
// runs as a single instance (no horizontal scaling — one container per
// deploy, compose.yml has no replicas); a per-process name meant a crash
// restart (same container, a fresh PID, or a fresh container with a new
// hostname) got a brand-new Redis consumer identity, and Redis consumer
// groups scope the pending-entries list to the consumer name. XREADGROUP
// id="0" only ever returns the CALLING consumer's own PEL, so a restarted
// process under a new name could never see messages the dead process had
// been delivered but not yet acked — they were silently stranded forever
// (the FR-40 invariant never recovered). Found by the Phase-5 chaos suite's
// kill-acct scenario. A fixed name makes every restart re-attach to the same
// PEL and recover it via the existing id="0" replay in runConsumer.
func consumerName() string { return "hikrad-acct" }

// Handler is the ingest + counters HTTP surface.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/acct", s.ingestHandler)
	mux.HandleFunc("/internal/acct/counters", s.countersHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// Run starts the consumer, reaper, spill drainer and counter flusher, and serves
// the HTTP endpoint until ctx is cancelled, then shuts down and flushes counters.
func (s *Service) Run(ctx context.Context) error {
	go s.runConsumer(ctx)
	go s.runReaper(ctx)
	go s.runSpillDrainer(ctx)
	go s.runCounterFlusher(ctx)

	srv := &http.Server{
		Addr:              s.httpAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("hikrad-acct ingest listening", "addr", s.httpAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("hikrad-acct shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		// Final counter flush so the durable mirror matches reality across the
		// restart (DoD: counters survive service restart).
		if err := s.counters.flush(context.Background(), s.db); err != nil {
			s.log.Warn("acct: final counter flush failed", "error", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// runSpillDrainer replays the disk WAL into the stream whenever Redis is
// reachable. Drained records rejoin the pipeline and dedup downstream if any
// were partially replayed.
func (s *Service) runSpillDrainer(ctx context.Context) {
	if s.spill == nil || !s.spill.enabled || s.rdb == nil {
		return
	}
	push := func(payload []byte) error {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if _, err := enqueueStream(cctx, s.rdb, payload); err != nil {
			return err
		}
		s.counters.drained.Add(1)
		return nil
	}
	drain := func() {
		drained, bad, err := s.spill.drain(push)
		if drained > 0 || bad > 0 {
			s.log.Info("acct spill drained", "drained", drained, "corrupt_skipped", bad)
		}
		if err != nil {
			s.log.Warn("acct spill drain paused", "error", err)
		}
	}
	drain() // recover anything left by a previous crash
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.spill.pending() > 0 {
				drain()
			}
		}
	}
}

func (s *Service) runCounterFlusher(ctx context.Context) {
	if s.db == nil {
		return
	}
	// 1s, not 5s: received/enqueued/spilled/drained are only ever made durable
	// here (unlike persisted/deduplicated/reaped, which now commit atomically
	// with the row they count — see counters.go's bumpPersistedInTx). Ingest
	// deliberately never touches Postgres synchronously (NFR-1: the ack path
	// must not depend on DB availability/latency), so a periodic flush is the
	// only way these survive an ungraceful crash; shrinking the tick bounds
	// how much a SIGKILL right before the next tick can permanently
	// under-report by, without adding any synchronous DB dependency to
	// ingest. Found by the Phase-5 chaos suite's kill-acct/unclean-reboot
	// scenarios: the counter invariant can go permanently (not just
	// transiently) inconsistent after a hard crash, even though the
	// underlying accounting data is never lost.
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.counters.flush(ctx, s.db); err != nil {
				s.log.Warn("acct: counter flush failed", "error", err)
			}
		}
	}
}
