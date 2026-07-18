package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// Server holds the daemon's process-lifetime state: at most one in-memory
// *run at a time (the fast path, see run.go), plus everything needed to
// resolve each verb. No database, no Redis — the daemon is deliberately
// dependency-free (source brief: "a small host daemon").
type Server struct {
	cfg config
	log *slog.Logger

	mu      sync.Mutex
	current *updateRun
}

func newServer(cfg config, log *slog.Logger) *Server {
	return &Server{cfg: cfg, log: log}
}

func (s *Server) serve(ln net.Listener) error {
	s.reconcileIfStale(s.log)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReaderSize(conn, 64*1024)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	var req Request
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(line)), &req); jsonErr != nil {
		writeLine(conn, ErrorResponse{OK: false, Error: "malformed request"})
		return
	}

	// FR-86.2/FR-88.1: every verb is token-gated, checked before the lock or
	// any subprocess is ever touched — including check/status, which are
	// read-only but still not anonymous.
	if !tokensEqual(req.Token, s.cfg.Token) {
		writeLine(conn, ErrorResponse{OK: false, Error: "unauthorized"})
		return
	}

	switch req.Verb {
	case "check":
		s.handleCheck(conn)
	case "update":
		s.handleUpdate(conn, req)
	case "status":
		s.handleStatus(conn)
	case "rollback-status":
		s.handleRollbackStatus(conn)
	default:
		writeLine(conn, ErrorResponse{OK: false, Error: "unknown verb"})
	}
}

// tokensEqual hashes both sides before comparing so the check is constant
// time regardless of the two strings' relative lengths (a bare
// subtle.ConstantTimeCompare on unequal-length inputs still leaks length).
func tokensEqual(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}

func (s *Server) handleCheck(conn net.Conn) {
	current := readVersionFile(s.cfg.VersionFile)
	avail, bundlePath := s.findNewerBundle(current)
	resp := CheckResponse{OK: true, CurrentVersion: current, DeliveryMode: s.cfg.DeliveryMode}
	if avail != "" {
		resp.AvailableVersion = strPtr(avail)
		resp.BundlePath = strPtr(bundlePath)
	}
	writeLine(conn, resp)
}

func (s *Server) handleUpdate(conn net.Conn, req Request) {
	var bundlePath string
	if req.BundlePath != "" {
		p, err := validateBundlePath(s.cfg.Root, req.BundlePath)
		if err != nil {
			writeLine(conn, ErrorResponse{OK: false, Error: "invalid bundle_path"})
			return
		}
		bundlePath = p
	}

	owner := req.Requester
	if owner == "" {
		owner = "socket"
	}

	r, started, lockOwner, startedAt := s.startUpdate(owner, bundlePath)
	if !started {
		resp := ErrorResponse{OK: false, Error: "locked"}
		if lockOwner != "" {
			resp.LockOwner = strPtr(lockOwner)
		}
		if startedAt != "" {
			resp.StartedAt = strPtr(startedAt)
		}
		writeLine(conn, resp)
		return
	}

	// The in-memory fast path only rules out a concurrent request to THIS
	// daemon instance — whether the child's own flock (C3) actually got the
	// real lock is known only once its first output line arrives.
	select {
	case <-r.resolved:
	case <-time.After(30 * time.Second):
		// The child produced no output at all for 30s — treat as a real
		// (if very slow) run rather than hang the caller indefinitely; the
		// stream will simply be quiet until something happens.
	}
	r.mu.Lock()
	conflict := r.lockConflict
	r.mu.Unlock()
	if conflict {
		writeLine(conn, ErrorResponse{OK: false, Error: "locked", LockOwner: strPtr("cli")})
		return
	}

	_ = conn.SetWriteDeadline(time.Time{})
	r.tail(conn)
}

func (s *Server) handleStatus(conn net.Conn) {
	s.reconcileIfStale(s.log)

	s.mu.Lock()
	cur := s.current
	s.mu.Unlock()
	if cur != nil {
		owner := cur.Owner
		started := cur.StartedAt.UTC().Format(time.RFC3339)
		writeLine(conn, StatusResponse{OK: true, Locked: true, LockOwner: &owner, Stage: cur.stageSnapshot(), StartedAt: &started})
		return
	}

	held, err := probeLock(s.cfg.LockPath)
	if err == nil && !held {
		writeLine(conn, StatusResponse{OK: true, Locked: false, Stage: "idle"})
		return
	}

	// Either the probe failed (treat conservatively as "can't confirm
	// idle") or something external holds it (an orphaned child from a dead
	// daemon instance, or the bare CLI) — report the last known stage from
	// disk rather than claiming idle.
	st := loadState(s.cfg.StatePath)
	stage := st.Stage
	if isTerminalStage(stage) {
		stage = "apply" // the lock is held but our own state thinks it's idle/done — a run this instance never tracked at all; "apply" is a reasonable non-committal in-progress label
	}
	writeLine(conn, StatusResponse{OK: true, Locked: true, LockOwner: strPtr("cli"), Stage: stage})
}

func (s *Server) handleRollbackStatus(conn net.Conn) {
	s.reconcileIfStale(s.log)

	st := loadState(s.cfg.StatePath)
	completedAt := ""
	if st.CompletedAt != nil {
		completedAt = st.CompletedAt.UTC().Format(time.RFC3339)
	}
	writeLine(conn, RollbackStatusResponse{
		OK: true, LastAction: st.LastAction, Result: st.LastResult,
		Version: st.LastVersion, CompletedAt: completedAt,
	})
}
