// Package perfutil holds the small amount of docker/process/DB plumbing
// shared by the perf tools that need a real hikrad-acct + Postgres/Redis
// (ingest, sizing) — the same standalone-container approach as
// backend/test/chaos (see its README for why: it sidesteps the Windows
// bind-mount issues that affect deploy/freeradius/caddy entirely).
package perfutil

import (
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

func dockerCmd(args ...string) error {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func DockerRm(name string) error {
	_ = dockerCmd("kill", name)
	return dockerCmd("rm", "-f", name)
}

// ProvisionPostgres starts a throwaway TimescaleDB container publishing the
// port parsed out of dbURL.
func ProvisionPostgres(name, dbURL string) error {
	_ = DockerRm(name)
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
		db = "hikrad_perf"
	}
	return dockerCmd("run", "-d", "--name", name,
		"-p", port+":5432",
		"-e", "POSTGRES_USER="+user,
		"-e", "POSTGRES_PASSWORD="+pass,
		"-e", "POSTGRES_DB="+db,
		"timescale/timescaledb:latest-pg16")
}

func ProvisionRedis(name, redisURL string) error {
	_ = DockerRm(name)
	u, err := url.Parse(redisURL)
	if err != nil {
		return err
	}
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	return dockerCmd("run", "-d", "--name", name,
		"-p", port+":6379",
		"redis:7-alpine",
		"redis-server", "--appendonly", "yes", "--appendfsync", "everysec")
}

func WaitTCP(hostport string, timeout time.Duration) error {
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

func HostPort(rawURL, defPort string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "127.0.0.1:" + defPort
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = defPort
	}
	return net.JoinHostPort(host, port)
}
