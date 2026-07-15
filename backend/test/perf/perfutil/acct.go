package perfutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
)

// Migrate retries the whole migrate-connect cycle: timescale/timescaledb's
// entrypoint restarts Postgres partway through its first-boot tuning, so a
// plain TCP-connect check can pass during the brief pre-restart window and
// still hand back a connection that dies mid-handshake (see
// backend/test/chaos/docker.go's migrateForChaos for the same fix).
func Migrate(dbURL, dir string) error {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if lastErr = platform.Migrate(dbURL, dir, log); lastErr == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("migrate retries exhausted: %w", lastErr)
}

// Acct manages a freshly-built hikrad-acct child process, mirroring
// backend/test/chaos's process.go.
type Acct struct {
	Addr     string
	binPath  string
	spillDir string
	cmd      *exec.Cmd
}

func BuildAcct(backendDir string) (*Acct, error) {
	tmp, err := os.MkdirTemp("", "hikrad-perf-acct-*")
	if err != nil {
		return nil, err
	}
	bin := filepath.Join(tmp, "hikrad-acct")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/hikrad-acct")
	cmd.Dir = backendDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go build hikrad-acct: %w: %s", err, string(out))
	}
	spill := filepath.Join(tmp, "spill")
	if err := os.MkdirAll(spill, 0o755); err != nil {
		return nil, err
	}
	return &Acct{binPath: bin, spillDir: spill}, nil
}

func (a *Acct) Start(addr, dbURL, redisURL string) error {
	a.Addr = addr
	cmd := exec.Command(a.binPath)
	cmd.Env = append(os.Environ(),
		"HIKRAD_DB_URL="+dbURL,
		"HIKRAD_REDIS_URL="+redisURL,
		"HIKRAD_ACCT_ADDR="+addr,
		"HIKRAD_ACCT_SPILL_DIR="+a.spillDir,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	a.cmd = cmd
	return a.waitHealthy(20 * time.Second)
}

func (a *Acct) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := "http://" + a.Addr + "/healthz"
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("hikrad-acct did not become healthy at %s within %s", url, timeout)
}

func (a *Acct) Stop() {
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
		_, _ = a.cmd.Process.Wait()
	}
	if a.binPath != "" {
		_ = os.RemoveAll(filepath.Dir(a.binPath))
	}
}
