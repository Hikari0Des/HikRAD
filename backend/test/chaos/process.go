package main

// hikrad-acct is managed as a plain child process (not a container): built
// once per run into a temp binary, then started/SIGKILLed/restarted
// directly. That models "kill hikrad-acct mid-flood" and "acct restart
// resumes backlog" (FR-37.5) with millisecond-precision control and no
// container overhead, while Postgres/Redis (the stateful dependencies the
// brief actually wants killed) run as real docker containers.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Rig is the shared environment every chaos scenario runs against.
type Rig struct {
	DBURL          string
	RedisURL       string
	AcctAddr       string
	MigrationsDir  string
	PGContainer    string
	RedisContainer string
	Sessions       int
	Rate           float64
	Duration       time.Duration
	KillFor        time.Duration
	Interims       int
	OutDir         string

	db  *pgxpool.Pool
	rdb *redis.Client

	binPath  string
	spillDir string
	cmd      *exec.Cmd
}

func (r *Rig) buildAcctBinary() error {
	tmp, err := os.MkdirTemp("", "hikrad-chaos-acct-*")
	if err != nil {
		return err
	}
	bin := filepath.Join(tmp, "hikrad-acct")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	// This tool is meant to be run from backend/ (`cd backend && go run
	// ./test/chaos ...`, matching every other test entrypoint in the repo),
	// so the build's working directory is inherited as-is.
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/hikrad-acct")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build hikrad-acct: %w: %s", err, string(out))
	}
	r.binPath = bin
	r.spillDir = filepath.Join(tmp, "spill")
	return os.MkdirAll(r.spillDir, 0o755)
}

func (r *Rig) cleanupBinary() {
	if r.cmd != nil {
		_ = r.killAcct()
	}
	if r.binPath != "" {
		_ = os.RemoveAll(filepath.Dir(r.binPath))
	}
}

// startAcct launches a fresh hikrad-acct process pointed at the rig's
// Postgres/Redis and spill dir, and waits for /healthz.
func (r *Rig) startAcct() error {
	cmd := exec.Command(r.binPath)
	cmd.Env = append(os.Environ(),
		"HIKRAD_DB_URL="+r.DBURL,
		"HIKRAD_REDIS_URL="+r.RedisURL,
		"HIKRAD_ACCT_ADDR="+r.AcctAddr,
		"HIKRAD_ACCT_SPILL_DIR="+r.spillDir,
		"HIKRAD_ACCT_INTERIM_SECS=30",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	r.cmd = cmd
	return r.waitAcctHealthy(20 * time.Second)
}

func (r *Rig) waitAcctHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := "http://" + r.AcctAddr + "/healthz"
	for time.Now().Before(deadline) {
		if err := httpOK(url); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("hikrad-acct did not become healthy at %s within %s", url, timeout)
}

// killAcct sends SIGKILL — an unclean crash, not a graceful shutdown, on
// purpose: it models the process dying mid-write with no chance to flush
// counters, which is the scenario the brief actually wants proven safe.
func (r *Rig) killAcct() error {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	err := r.cmd.Process.Kill()
	_, _ = r.cmd.Process.Wait()
	r.cmd = nil
	return err
}

func httpOK(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
