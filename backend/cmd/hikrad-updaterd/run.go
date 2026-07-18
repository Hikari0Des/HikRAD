package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// run tracks one in-flight `update` invocation. Enforcement of "only one
// update at a time" is NOT this struct's job (C3) — that's scripts/hikrad's
// own flock, held by the spawned child process for the child's own lifetime,
// which is what makes the guarantee survive the daemon dying mid-update
// (FR-86.5). This struct is purely: (a) an in-memory fast path so a second
// request to the *same* daemon instance doesn't spawn a doomed-to-fail child
// for no reason, and (b) the live tail source for a connection watching an
// update it started.
type updateRun struct {
	mu     sync.Mutex
	stage  string
	events []Event

	doneCh    chan struct{}
	Owner     string
	StartedAt time.Time

	previousVersion string
	targetVersion   string

	// resolved closes once the daemon knows whether this is a genuine run
	// (proceed to tail) or a lock conflict (the child's own flock refused,
	// C3) — that distinction is only knowable after reading the child's
	// first output line, which happens asynchronously in runUpdate's
	// goroutine, strictly after startUpdate has already returned to the
	// connection handler. lockConflict is valid only once resolved is closed.
	resolved     chan struct{}
	lockConflict bool
}

func (r *updateRun) setStage(stage string) {
	r.mu.Lock()
	r.stage = stage
	r.mu.Unlock()
}

func (r *updateRun) stageSnapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stage
}

func (r *updateRun) append(e Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

// tail writes every event from the start of the run, then polls for new
// ones until doneCh closes. A write failure (the client disconnected) simply
// ends the tail — it never affects the run itself, which is exactly the
// "panel's own container gets replaced mid-update" case FR-86.5/FR-87.2 are
// built around.
func (r *updateRun) tail(w io.Writer) {
	idx := 0
	for {
		r.mu.Lock()
		pending := append([]Event(nil), r.events[idx:]...)
		idx = len(r.events)
		r.mu.Unlock()
		for _, e := range pending {
			if writeEventLine(w, e) != nil {
				return
			}
		}
		select {
		case <-r.doneCh:
			r.mu.Lock()
			rest := append([]Event(nil), r.events[idx:]...)
			r.mu.Unlock()
			for _, e := range rest {
				_ = writeEventLine(w, e)
			}
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// writeEventLine is tail's own write path, distinct from writeLine (used by
// the single-shot verb responses): tail needs the real Write error to know
// when to stop trying — a dead client connection must never be mistaken for
// the run itself failing (FR-86.5's whole point).
func writeEventLine(w io.Writer, e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// startUpdate is the fast in-memory path (see run's doc comment). It never
// touches the lock file — if this daemon instance already has a run object,
// that's authoritative for THIS instance without any I/O.
func (s *Server) startUpdate(owner, bundlePath string) (r *updateRun, started bool, lockOwner, startedAt string) {
	s.mu.Lock()
	if s.current != nil {
		cur := s.current
		s.mu.Unlock()
		return nil, false, cur.Owner, cur.StartedAt.UTC().Format(time.RFC3339)
	}
	prevVersion := readVersionFile(s.cfg.VersionFile)
	newRun := &updateRun{
		doneCh:          make(chan struct{}),
		resolved:        make(chan struct{}),
		Owner:           owner,
		StartedAt:       time.Now().UTC(),
		previousVersion: prevVersion,
		targetVersion:   bundleTargetVersion(bundlePath),
		stage:           "backup",
	}
	s.current = newRun
	s.mu.Unlock()

	_ = saveState(s.cfg.StatePath, State{
		Stage:           "backup",
		LockOwner:       owner,
		StartedAt:       timePtr(newRun.StartedAt),
		PreviousVersion: prevVersion,
		TargetVersion:   newRun.targetVersion,
	})
	go s.runUpdate(newRun, bundlePath)
	return newRun, true, "", ""
}

// lockedLinePattern matches scripts/hikrad's cmd_update failing its own
// flock acquisition (C3) — the ONE line that means "someone else already
// holds the real lock," as opposed to any other failure. Matched against the
// child's very first output line: a real lock conflict fails before any
// other stage line is ever printed.
var lockedLinePattern = regexp.MustCompile(`already in progress \(locked\)`)

func classifyStage(line string) string {
	switch {
	case strings.Contains(line, "Pre-update backup"):
		return "backup"
	case strings.Contains(line, "Verifying release bundle"):
		return "apply"
	case strings.Contains(line, "Building images"):
		return "apply"
	case strings.Contains(line, "Applying update"):
		return "health_check"
	case strings.Contains(line, "did not become healthy"):
		return "health_check"
	case strings.Contains(line, "rolling back"):
		return "rolling_back"
	}
	return ""
}

func stripErrorPrefix(line string) string {
	line = strings.TrimSpace(line)
	if i := strings.Index(line, "ERROR: "); i >= 0 {
		return line[i+len("ERROR: "):]
	}
	if line == "" {
		return "update failed with no output"
	}
	return line
}

func readVersionFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// runUpdate executes the `hikrad update` child and relays its output as
// stage events, deliberately detached from any connection: once started it
// runs to completion (or until the child itself dies) regardless of whether
// anyone is still watching (FR-86.5). It is the ONE place state.json gets
// written for a run this daemon instance is actively tracking.
func (s *Server) runUpdate(r *updateRun, bundlePath string) {
	defer func() {
		close(r.doneCh)
		s.mu.Lock()
		if s.current == r {
			s.current = nil
		}
		s.mu.Unlock()
	}()

	args := []string{"update"}
	if bundlePath != "" {
		args = append(args, "--bundle", bundlePath)
	}
	cmd := exec.Command(s.cfg.UpdateCmd, args...)

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		close(r.resolved) // not a lock conflict — the connection handler proceeds to tail and sees this result
		r.setStage("rolled_back")
		r.append(Event{Type: "result", OK: boolPtr(false), Message: "failed to start update: " + err.Error()})
		_ = saveState(s.cfg.StatePath, State{
			Stage: "rolled_back", LastAction: "update", LastResult: "failed",
			LastVersion: r.previousVersion, CompletedAt: timePtr(time.Now().UTC()),
			PreviousVersion: r.previousVersion, TargetVersion: r.targetVersion,
		})
		return
	}

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- cmd.Wait()
		_ = pw.Close()
	}()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	stage := ""
	lastLine := ""
	sawSuccess := false
	sawLockConflict := false
	firstLine := true

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lastLine = line

		if firstLine {
			firstLine = false
			if lockedLinePattern.MatchString(line) {
				sawLockConflict = true
				r.mu.Lock()
				r.lockConflict = true
				r.mu.Unlock()
			}
			close(r.resolved)
		}

		if sawLockConflict {
			// Drain the rest of the child's (brief) output without treating
			// it as a stage transition — there won't be any, but a future
			// change to the lock-conflict message must not accidentally
			// masquerade as a real stage line.
			continue
		}

		if newStage := classifyStage(line); newStage != "" && newStage != stage {
			stage = newStage
			r.setStage(stage)
			r.append(Event{Type: "stage", Stage: stage, TS: time.Now().UTC().Format(time.RFC3339)})
			_ = saveState(s.cfg.StatePath, State{
				Stage: stage, LockOwner: r.Owner, StartedAt: timePtr(r.StartedAt),
				PreviousVersion: r.previousVersion, TargetVersion: r.targetVersion,
			})
		}
		if strings.Contains(line, "Update complete and healthy") {
			sawSuccess = true
		}
	}
	if firstLine {
		// The child exited without printing anything at all — not the
		// lock-conflict message (that's always at least one line), so
		// treat it as an ordinary (if unusual) failure once resolved.
		close(r.resolved)
	}
	waitErr := <-waitErrCh

	if sawLockConflict {
		// The child never actually ran an update — its own flock refused
		// before touching anything. No "result" event goes into this run's
		// stream — the caller sees it via the ordinary locked ErrorResponse
		// instead (handleUpdate checks lockConflict before treating this as
		// a started run). startUpdate already wrote an initial state.json
		// "backup" entry before this was known to be a conflict; leaving it
		// as-is is harmless — the next status/reconcileIfStale call finds
		// the lock free and the version unchanged, and correctly resolves
		// it to "rolled_back" (nothing changed, which is honestly true).
		return
	}

	version := readVersionFile(s.cfg.VersionFile)
	ok := waitErr == nil && sawSuccess

	var finalStage, lastResult, message string
	if ok {
		finalStage = "done"
		lastResult = "success"
		message = "Update complete and healthy."
	} else {
		finalStage = "rolled_back"
		message = stripErrorPrefix(lastLine)
		if stage == "rolling_back" {
			lastResult = "rolled_back"
		} else {
			lastResult = "failed"
		}
	}
	r.setStage(finalStage)
	r.append(Event{Type: "result", OK: boolPtr(ok), Version: version, Message: message})

	_ = saveState(s.cfg.StatePath, State{
		Stage: finalStage, LastAction: "update", LastResult: lastResult,
		LastVersion: version, CompletedAt: timePtr(time.Now().UTC()),
		PreviousVersion: r.previousVersion, TargetVersion: r.targetVersion,
	})
}

// reconcileIfStale resolves a state.json this daemon instance never
// observed the end of — the case gate item 5 exists for: the daemon died
// mid-update, and a later daemon (this process, possibly a restart) is now
// being asked for status. If nothing currently holds the lock, the run that
// set this state has already finished one way or another; compare the live
// VersionFile against the recorded Previous/TargetVersion to resolve it
// (success / rolled_back / ambiguous-failed) rather than leaving status
// stuck reporting a stage that stopped advancing hours ago.
func (s *Server) reconcileIfStale(log *slog.Logger) {
	s.mu.Lock()
	active := s.current != nil
	s.mu.Unlock()
	if active {
		return
	}

	st := loadState(s.cfg.StatePath)
	if isTerminalStage(st.Stage) {
		return
	}

	held, err := probeLock(s.cfg.LockPath)
	if err != nil || held {
		// Either we can't tell, or something still genuinely holds the
		// lock (an orphaned child from a dead daemon instance, still
		// actually running) — leave the stale-but-honest state as-is.
		return
	}

	current := readVersionFile(s.cfg.VersionFile)
	resolved := State{
		Stage: "rolled_back", LastAction: "update", LastResult: "failed",
		LastVersion: current, CompletedAt: timePtr(time.Now().UTC()),
	}
	switch {
	case st.TargetVersion != "" && current == st.TargetVersion:
		resolved.Stage, resolved.LastResult = "done", "success"
	case st.PreviousVersion != "" && current == st.PreviousVersion:
		resolved.Stage, resolved.LastResult = "rolled_back", "rolled_back"
	}
	if log != nil {
		log.Info("hikrad-updaterd: reconciled a stale in-progress state after restart",
			"previous_stage", st.Stage, "resolved_result", resolved.LastResult, "version", current)
	}
	_ = saveState(s.cfg.StatePath, resolved)
}

// --- check verb: bundle-directory scan ---------------------------------

var semverRe = regexp.MustCompile(`v?([0-9]+)\.([0-9]+)\.([0-9]+)`)

func parseSemver(s string) [3]int {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return [3]int{0, 0, 0}
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	return [3]int{a, b, c}
}

func semverGreater(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// findNewerBundle scans IncomingDir for the highest hikrad-vX.Y.Z.tar strictly
// greater than the currently-running version. Never touches the network
// (NFR-7/C2's own note: `check` is a directory scan, not a fetch).
func (s *Server) findNewerBundle(current string) (version, path string) {
	entries, err := os.ReadDir(s.cfg.IncomingDir)
	if err != nil {
		return "", ""
	}
	curV := parseSemver(current)
	bestV := curV
	bestName := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := bundleFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		v := [3]int{a, b, c}
		if semverGreater(v, bestV) {
			bestV = v
			bestName = e.Name()
		}
	}
	if bestName == "" {
		return "", ""
	}
	return bundleTargetVersion(bestName), filepath.Join(s.cfg.IncomingDir, bestName)
}
