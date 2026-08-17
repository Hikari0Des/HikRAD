package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// config is hikrad-acct's own minimal environment. It deliberately does NOT
// reuse platform.LoadConfig: this binary neither decrypts subscriber secrets nor
// issues JWTs, so requiring HIKRAD_ENCRYPTION_KEY / HIKRAD_JWT_SECRET here would
// force operators to inject credentials the ingest service has no use for. Only
// the DB and Redis URLs are shared with the C5 contract.
type config struct {
	DBURL    string
	RedisURL string
	Addr     string
	SpillDir string
	Interim  time.Duration
}

func loadConfig() (config, error) {
	var cfg config
	var missing []string
	need := func(name string) string {
		v := os.Getenv(name)
		if v == "" {
			missing = append(missing, name)
		}
		return v
	}
	cfg.DBURL = need("HIKRAD_DB_URL")
	cfg.RedisURL = need("HIKRAD_REDIS_URL")
	if len(missing) > 0 {
		return config{}, fmt.Errorf("hikrad-acct: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	cfg.Addr = os.Getenv("HIKRAD_ACCT_ADDR")
	if cfg.Addr == "" {
		// 127.0.0.1, not ":8082"/0.0.0.0 (v4-2, FR-118) — same reasoning as
		// hikrad-api's own loopback bind (cmd/hikrad-api/main.go): under
		// Docker this port was never published to the host at all
		// (`expose: "8082"`, reachable only from FreeRADIUS's own
		// container); natively there is no network-namespace isolation to
		// lean on, and the only legitimate caller is FreeRADIUS's
		// accounting.pl exec script running on this same host.
		cfg.Addr = "127.0.0.1:8082"
	}
	// Default matches the compose volume mount (deploy/compose.yml hikrad-acct
	// block mounts the spill volume at /spill).
	cfg.SpillDir = os.Getenv("HIKRAD_ACCT_SPILL_DIR")
	if cfg.SpillDir == "" {
		cfg.SpillDir = "/spill"
	}

	cfg.Interim = 300 * time.Second
	if raw := os.Getenv("HIKRAD_ACCT_INTERIM_SECS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return config{}, fmt.Errorf("hikrad-acct: HIKRAD_ACCT_INTERIM_SECS must be a positive integer, got %q", raw)
		}
		cfg.Interim = time.Duration(n) * time.Second
	}
	return cfg, nil
}
