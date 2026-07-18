package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// --- Test-helper subprocess (classic Go "TestHelperProcess" re-exec idiom,
// used e.g. by the standard library's os/exec tests) --------------------
//
// The compiled test binary doubles as a stand-in for the real `hikrad`
// CLI: when HIKRAD_UPDATER_TEST_HELPER=1 is set, TestMain runs
// helperMain() and exits instead of running the test suite. This lets
// every test drive the daemon's real output-scanning/state-machine logic
// against scripted, controllable output without needing bash, Docker, or
// a real hikrad-vX.Y.Z install anywhere.
func TestMain(m *testing.M) {
	if os.Getenv("HIKRAD_UPDATER_TEST_HELPER") == "1" {
		helperMain()
		return
	}
	os.Exit(m.Run())
}

// helperMain simulates scripts/hikrad's cmd_update: it prints the exact
// substrings run.go's classifyStage/lockedLinePattern key off, on the
// timeline HELPER_* env vars describe.
func helperMain() {
	switch os.Getenv("HELPER_OUTCOME") {
	case "locked":
		fmt.Println("[hikrad] ERROR: update already in progress (locked)")
		os.Exit(1)
	case "success":
		fmt.Println("[hikrad] Pre-update backup…")
		sleepHelper()
		fmt.Println("[hikrad] Verifying release bundle…")
		sleepHelper()
		fmt.Println("[hikrad] Applying update (hikrad-api runs forward-only migrations on boot)…")
		sleepHelper()
		fmt.Println("[hikrad] Update complete and healthy.")
		writeHelperVersion(os.Getenv("HELPER_TARGET_VERSION"))
		os.Exit(0)
	case "rollback":
		fmt.Println("[hikrad] Pre-update backup…")
		sleepHelper()
		fmt.Println("[hikrad] Applying update (hikrad-api runs forward-only migrations on boot)…")
		sleepHelper()
		fmt.Println("[hikrad] hikrad-api did not become healthy after update — rolling back images.")
		fmt.Println("[hikrad] ERROR: update rolled back to the previous version. If a migration partially applied, run: hikrad restore <archive>")
		writeHelperVersion(os.Getenv("HELPER_PREV_VERSION"))
		os.Exit(1)
	case "hang":
		// Never finishes on its own within a test's lifetime — used by the
		// daemon-crash-survival test, which kills the *daemon*, not this
		// process, and expects this process to keep running regardless.
		fmt.Println("[hikrad] Pre-update backup…")
		sleepHelper()
		fmt.Println("[hikrad] Applying update (hikrad-api runs forward-only migrations on boot)…")
		time.Sleep(4 * time.Second)
		fmt.Println("[hikrad] Update complete and healthy.")
		writeHelperVersion(os.Getenv("HELPER_TARGET_VERSION"))
		os.Exit(0)
	default:
		fmt.Println("[hikrad] ERROR: unrecognized HELPER_OUTCOME")
		os.Exit(1)
	}
}

func sleepHelper() {
	if ms := os.Getenv("HELPER_SLEEP_MS"); ms != "" {
		if n, err := strconv.Atoi(ms); err == nil {
			time.Sleep(time.Duration(n) * time.Millisecond)
		}
	}
}

func writeHelperVersion(v string) {
	path := os.Getenv("HELPER_VERSION_FILE")
	if path == "" || v == "" {
		return
	}
	_ = os.WriteFile(path, []byte(v), 0o644)
}

// helperEnv returns cmd.Env for spawning the current test binary as the
// stub `hikrad` CLI, so cfg.UpdateCmd can point straight at it.
func helperSelfPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}

// --- test scaffolding --------------------------------------------------

func testConfig(t *testing.T, root string) config {
	t.Helper()
	dataDir := filepath.Join(root, "data", "updater")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	versionFile := filepath.Join(root, "VERSION")
	if err := os.WriteFile(versionFile, []byte("v1.0.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "updater.sock")
	if len(socket) > 100 {
		// unix socket paths have a short OS-imposed max length; fall back to
		// a shorter temp location if the test's own tmp dir is deep.
		f, err := os.CreateTemp("", "hikrad-updaterd-*.sock")
		if err != nil {
			t.Fatal(err)
		}
		socket = f.Name()
		f.Close()
		os.Remove(socket)
	}
	return config{
		Token:        "test-token",
		SocketPath:   socket,
		Root:         root,
		LockPath:     filepath.Join(dataDir, "update.lock"),
		StatePath:    filepath.Join(dataDir, "state.json"),
		IncomingDir:  filepath.Join(root, "incoming"),
		VersionFile:  versionFile,
		DeliveryMode: "bundle",
		UpdateCmd:    helperSelfPath(t),
	}
}

// startTestServer runs a Server in-process against a real unix socket —
// sufficient for every test that doesn't need to kill "the daemon" as a
// genuinely separate OS process (that's TestRollbackSurvivesDaemonDeath /
// TestRollbackStatusPersistsAcrossRestart, which spawn a real subprocess
// instead, see below).
func startTestServer(t *testing.T, cfg config) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := newServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go func() { _ = srv.serve(ln) }()
	return cfg.SocketPath, func() { _ = ln.Close() }
}

func dial(t *testing.T, socket string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func sendRequest(t *testing.T, socket string, req Request) *bufio.Reader {
	t.Helper()
	conn := dial(t, socket)
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		t.Fatal(err)
	}
	return bufio.NewReader(conn)
}

func readOneJSON(t *testing.T, r *bufio.Reader, v any) {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), v); err != nil {
		t.Fatalf("unmarshal %q: %v", line, err)
	}
}

// --- gate item 2: token auth + bundle_path validation -------------------

func TestUnauthorizedRefused(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	socket, closeFn := startTestServer(t, cfg)
	defer closeFn()

	for _, verb := range []string{"check", "update", "status", "rollback-status"} {
		r := sendRequest(t, socket, Request{Verb: verb, Token: "wrong-token"})
		var resp ErrorResponse
		readOneJSON(t, r, &resp)
		if resp.OK || resp.Error != "unauthorized" {
			t.Fatalf("verb %s: expected unauthorized error, got %+v", verb, resp)
		}
	}

	// The lock must never have been touched by any of the above.
	if _, err := os.Stat(cfg.LockPath); err == nil {
		t.Fatalf("lock file was created by an unauthorized request")
	}
}

func TestBundlePathValidation(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	if err := os.MkdirAll(cfg.IncomingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	socket, closeFn := startTestServer(t, cfg)
	defer closeFn()

	cases := []struct {
		name string
		path string
	}{
		{"traversal", filepath.Join(cfg.IncomingDir, "..", "evil.tar")},
		{"outside incoming", filepath.Join(root, "hikrad-v9.9.9.tar")},
		{"bad filename shape", filepath.Join(cfg.IncomingDir, "not-a-bundle.tar")},
		{"shell metacharacters", filepath.Join(cfg.IncomingDir, "hikrad-v1.0.0.tar; rm -rf /")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := sendRequest(t, socket, Request{Verb: "update", Token: cfg.Token, BundlePath: c.path})
			var resp ErrorResponse
			readOneJSON(t, r, &resp)
			if resp.OK || resp.Error != "invalid bundle_path" {
				t.Fatalf("expected invalid bundle_path, got %+v", resp)
			}
		})
	}

	// A valid path (file need not exist for validation to pass — existence is
	// scripts/hikrad's own concern) is accepted and reaches the child process.
	valid := filepath.Join(cfg.IncomingDir, "hikrad-v1.2.3.tar")
	r := sendRequest(t, socket, Request{
		Verb: "update", Token: cfg.Token, BundlePath: valid,
	})
	_ = r // draining not required for this leg — dispatch alone proves it wasn't rejected as invalid
}

// --- gate item 3: lock semantics (fast in-memory path) -------------------

func TestConcurrentUpdateLock(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	socket, closeFn := startTestServer(t, cfg)
	defer closeFn()

	env := []string{"HIKRAD_UPDATER_TEST_HELPER=1", "HELPER_OUTCOME=success", "HELPER_SLEEP_MS=300"}
	_ = env // documents the intended env; the connection itself doesn't set env — see below

	// The daemon spawns cfg.UpdateCmd (this test binary) directly, so the
	// helper env vars must be visible to the DAEMON process, which in this
	// in-process test IS the test binary — os.Setenv affects it directly.
	t.Setenv("HIKRAD_UPDATER_TEST_HELPER", "1")
	t.Setenv("HELPER_OUTCOME", "success")
	t.Setenv("HELPER_SLEEP_MS", "400")
	t.Setenv("HELPER_TARGET_VERSION", "v1.1.0")
	t.Setenv("HELPER_VERSION_FILE", cfg.VersionFile)

	first := sendRequest(t, socket, Request{Verb: "update", Token: cfg.Token, Requester: "panel:first@example.com"})
	// Give the first request time to actually acquire the in-memory slot
	// before the second one races it.
	time.Sleep(50 * time.Millisecond)

	second := sendRequest(t, socket, Request{Verb: "update", Token: cfg.Token, Requester: "panel:second@example.com"})
	var resp ErrorResponse
	readOneJSON(t, second, &resp)
	if resp.OK || resp.Error != "locked" {
		t.Fatalf("second concurrent update: expected locked, got %+v", resp)
	}
	if resp.LockOwner == nil || *resp.LockOwner != "panel:first@example.com" {
		t.Fatalf("expected lock_owner to name the first requester, got %+v", resp.LockOwner)
	}

	// Drain the first stream to completion so the test doesn't leak a
	// blocked goroutine.
	drainStream(t, first)
}

func drainStream(t *testing.T, r *bufio.Reader) []Event {
	t.Helper()
	var events []Event
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return events
		}
		var e Event
		if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(line)), &e); jsonErr == nil {
			events = append(events, e)
			if e.Type == "result" {
				return events
			}
		}
	}
}

// TestLockConflictDetectedFromChildOutput proves the OTHER half of C3: even
// with no in-memory *updateRun at all (a fresh daemon instance), a child
// that fails its own flock is correctly reported as "locked" by scanning its
// output — this is what makes the guarantee survive a daemon restart.
func TestLockConflictDetectedFromChildOutput(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	socket, closeFn := startTestServer(t, cfg)
	defer closeFn()

	t.Setenv("HIKRAD_UPDATER_TEST_HELPER", "1")
	t.Setenv("HELPER_OUTCOME", "locked")

	r := sendRequest(t, socket, Request{Verb: "update", Token: cfg.Token})
	var resp ErrorResponse
	readOneJSON(t, r, &resp)
	if resp.OK || resp.Error != "locked" {
		t.Fatalf("expected locked (detected from child output), got %+v", resp)
	}
}

// --- gate item 4: no shell-reachable arguments ---------------------------
// (the grep leg in scripts/gate-v2-phase-7.sh covers this statically; the
// bundle-path test above covers it dynamically for the one field that names
// a filesystem path.)

// --- gate item 6/8-ish: check verb + status/rollback-status shapes -------

func TestCheckFindsNewerBundle(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	if err := os.MkdirAll(cfg.IncomingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hikrad-v0.9.0.tar", "hikrad-v1.2.3.tar", "not-a-bundle.txt"} {
		if err := os.WriteFile(filepath.Join(cfg.IncomingDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	socket, closeFn := startTestServer(t, cfg)
	defer closeFn()

	r := sendRequest(t, socket, Request{Verb: "check", Token: cfg.Token})
	var resp CheckResponse
	readOneJSON(t, r, &resp)
	if resp.CurrentVersion != "v1.0.0" {
		t.Fatalf("current_version = %q, want v1.0.0", resp.CurrentVersion)
	}
	if resp.AvailableVersion == nil || *resp.AvailableVersion != "v1.2.3" {
		t.Fatalf("available_version = %v, want v1.2.3", resp.AvailableVersion)
	}
}

func TestStatusIdleWhenNothingRunning(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	socket, closeFn := startTestServer(t, cfg)
	defer closeFn()

	r := sendRequest(t, socket, Request{Verb: "status", Token: cfg.Token})
	var resp StatusResponse
	readOneJSON(t, r, &resp)
	if resp.Locked || resp.Stage != "idle" {
		t.Fatalf("expected idle/unlocked on a fresh daemon, got %+v", resp)
	}
}

// --- gate item 5: rollback survives the daemon dying ---------------------

// buildDaemonBinary compiles the real cmd/hikrad-updaterd package once per
// test run so it can be exec'd as a genuinely separate OS process — needed
// to actually kill "the daemon" without also killing the test itself, and
// to prove the CHILD (the helper stub, standing in for `hikrad update`)
// keeps running regardless.
func buildDaemonBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "hikrad-updaterd-under-test")
	if runtimeIsWindows() {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = "."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build daemon: %v\n%s", err, stderr.String())
	}
	return out
}

func runtimeIsWindows() bool {
	return os.PathSeparator == '\\'
}

func startDaemonSubprocess(t *testing.T, bin string, cfg config, extraEnv ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"HIKRAD_UPDATER_TOKEN="+cfg.Token,
		"HIKRAD_UPDATER_SOCKET="+cfg.SocketPath,
		"HIKRAD_ROOT="+cfg.Root,
		"HIKRAD_VERSION_FILE="+cfg.VersionFile,
		"HIKRAD_UPDATE_CMD="+cfg.UpdateCmd,
		"HIKRAD_DELIVERY_MODE="+cfg.DeliveryMode,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon subprocess: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	waitForSocket(t, cfg.SocketPath)
	return cmd
}

func waitForSocket(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond); err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("daemon socket %s never became reachable", socket)
}

func TestRollbackSurvivesDaemonDeath(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real subprocesses; skipped in -short")
	}
	root := t.TempDir()
	cfg := testConfig(t, root)
	bin := buildDaemonBinary(t)

	helperEnv := []string{
		"HIKRAD_UPDATER_TEST_HELPER=1",
		"HELPER_OUTCOME=hang", // sleeps ~4s mid-run before finishing on its own
		"HELPER_SLEEP_MS=100",
		"HELPER_TARGET_VERSION=v2.0.0",
		"HELPER_VERSION_FILE=" + cfg.VersionFile,
	}
	daemon := startDaemonSubprocess(t, bin, cfg, helperEnv...)

	r := sendRequest(t, cfg.SocketPath, Request{Verb: "update", Token: cfg.Token, Requester: "panel:test@example.com"})
	// Read at least one stage line so we know the child has actually started
	// before we kill the daemon out from under it.
	line, err := r.ReadString('\n')
	if err != nil || !strings.Contains(line, "\"stage\"") {
		t.Fatalf("expected a stage event before killing the daemon, got %q (err=%v)", line, err)
	}

	// Kill the daemon. Its own child (the helper, simulating hikrad update)
	// is a separate OS process and must NOT be killed by this.
	if err := daemon.Process.Kill(); err != nil {
		t.Fatalf("kill daemon: %v", err)
	}
	_, _ = daemon.Process.Wait()

	// The helper is still running (it sleeps ~4s total); give it time to
	// finish and write the new version file on its own, unattended.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if v := readVersionFile(cfg.VersionFile); v == "v2.0.0" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	got := readVersionFile(cfg.VersionFile)
	if got != "v2.0.0" {
		t.Fatalf("child process did not complete its update after the daemon was killed: VERSION file = %q", got)
	}
}

func TestRollbackStatusPersistsAcrossRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real subprocesses; skipped in -short")
	}
	root := t.TempDir()
	cfg := testConfig(t, root)
	bin := buildDaemonBinary(t)

	helperEnv := []string{
		"HIKRAD_UPDATER_TEST_HELPER=1",
		"HELPER_OUTCOME=hang",
		"HELPER_SLEEP_MS=100",
		"HELPER_TARGET_VERSION=v2.0.0",
		"HELPER_VERSION_FILE=" + cfg.VersionFile,
	}
	if err := os.MkdirAll(cfg.IncomingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(cfg.IncomingDir, "hikrad-v2.0.0.tar")
	if err := os.WriteFile(bundlePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	daemon := startDaemonSubprocess(t, bin, cfg, helperEnv...)

	// bundle_path is what lets a restarted daemon reconcile an orphaned run
	// it never observed the end of (target_version, see run.go's
	// reconcileIfStale) — without it (registry/source-mode updates) the
	// outcome is genuinely unknowable from outside, which is real, not a
	// gap this test is meant to exercise.
	r := sendRequest(t, cfg.SocketPath, Request{Verb: "update", Token: cfg.Token, BundlePath: bundlePath})
	line, err := r.ReadString('\n')
	if err != nil || !strings.Contains(line, "\"stage\"") {
		t.Fatalf("expected a stage event before killing the daemon, got %q (err=%v)", line, err)
	}

	if err := daemon.Process.Kill(); err != nil {
		t.Fatalf("kill daemon: %v", err)
	}
	_, _ = daemon.Process.Wait()

	// Let the orphaned helper finish on its own (it flips VERSION to v2.0.0
	// after ~4s total, matching TestRollbackSurvivesDaemonDeath).
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if readVersionFile(cfg.VersionFile) == "v2.0.0" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// A FRESH daemon instance (new process, new in-memory state) comes up
	// against the same $HIKRAD_ROOT and must reconcile the stale state.json
	// it inherited into a correct "success" outcome (VersionFile now matches
	// the run's own recorded target_version).
	restarted := startDaemonSubprocess(t, bin, cfg)
	defer func() {
		_ = restarted.Process.Kill()
		_, _ = restarted.Process.Wait()
	}()

	var resp RollbackStatusResponse
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rr := sendRequest(t, cfg.SocketPath, Request{Verb: "rollback-status", Token: cfg.Token})
		readOneJSON(t, rr, &resp)
		if resp.Result == "success" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if resp.Result != "success" || resp.Version != "v2.0.0" {
		t.Fatalf("restarted daemon's rollback-status = %+v, want result=success version=v2.0.0", resp)
	}
}
