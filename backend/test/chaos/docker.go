package main

// Container lifecycle helpers. Postgres and Redis run as standalone `docker
// run` containers (published host ports, no bind-mounted config trees) so
// this rig works the same on the pilot's Linux host and on a Windows dev
// box — it deliberately avoids deploy/compose.yml's bind-mounted
// FreeRADIUS/Caddy config trees, which are a documented source of
// Windows-host permission failures unrelated to anything this suite tests.
// hikrad-acct itself runs as a plain child process (process.go) so it can be
// SIGKILLed and restarted in milliseconds without any container overhead.

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
)

func dockerCmd(args ...string) error {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func dockerKill(name string) error   { return dockerCmd("kill", name) }
func dockerStop(name string) error   { return dockerCmd("stop", "-t", "3", name) }
func dockerStart(name string) error  { return dockerCmd("start", name) }
func dockerRestart(name string) error { return dockerCmd("restart", "-t", "3", name) }
func dockerRm(name string) error {
	_ = dockerCmd("kill", name)
	return dockerCmd("rm", "-f", name)
}

// provisionPostgres starts a throwaway TimescaleDB container matching the
// compose stack's image, publishing the port parsed out of dbURL so the
// rest of the tool (and `go test`'s HIKRAD_TEST_DB_URL convention) can reach
// it as plain localhost.
func provisionPostgres(name, dbURL string) error {
	_ = dockerRm(name)
	u, err := url.Parse(dbURL)
	if err != nil {
		return err
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	user, pass := "hikrad", "hikrad"
	if u.User != nil {
		user = u.User.Username()
		if p, ok := u.User.Password(); ok {
			pass = p
		}
	}
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		db = "hikrad_chaos"
	}
	return dockerCmd("run", "-d", "--name", name,
		"-p", port+":5432",
		"-e", "POSTGRES_USER="+user,
		"-e", "POSTGRES_PASSWORD="+pass,
		"-e", "POSTGRES_DB="+db,
		"timescale/timescaledb:latest-pg16")
}

// provisionRedis starts a throwaway Redis container. fsync selects the AOF
// policy: "everysec" matches deploy/compose.yml's current shipped default;
// "always" is the docs/evidence/redis-durability-decision.md recommendation
// — pass -redis-fsync to compare them directly via the same scenario.
func provisionRedis(name, redisURL, fsync string) error {
	_ = dockerRm(name)
	u, err := url.Parse(redisURL)
	if err != nil {
		return err
	}
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	if fsync == "" {
		fsync = "everysec"
	}
	return dockerCmd("run", "-d", "--name", name,
		"-p", port+":6379",
		"redis:7-alpine",
		"redis-server", "--appendonly", "yes", "--appendfsync", fsync)
}

func waitTCP(hostport string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", hostport, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s: %w", hostport, lastErr)
}

func pgHostPort(dbURL string) string {
	u, err := url.Parse(dbURL)
	if err != nil {
		return "127.0.0.1:5432"
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	return net.JoinHostPort(host, port)
}

func redisHostPort(redisURL string) string {
	u, err := url.Parse(redisURL)
	if err != nil {
		return "127.0.0.1:6379"
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	return net.JoinHostPort(host, port)
}

// migrateForChaos retries the whole migrate-connect cycle for a while:
// timescale/timescaledb's entrypoint runs `timescaledb-tune` on first boot
// and restarts Postgres partway through, so a plain "TCP connect succeeded"
// check (waitTCP) can pass during the brief pre-restart accept window and
// still hand back a connection that dies mid-handshake.
func migrateForChaos(dbURL, dir string) error {
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
