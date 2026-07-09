// Package platform owns process configuration, DB/Redis constructors, the
// boot-time migration runner, and the typed Settings service (Phase 1,
// Agent A; FR-53, contract C5/C6). Domain packages receive its outputs via
// the httpapi Deps struct (contract C3); nothing here registers HTTP routes.
package platform

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// Config is the typed view of the frozen C5 environment contract. All three
// binaries (hikrad-api, hikrad-acct, hikrad-monitor) load it the same way.
type Config struct {
	// DBURL is HIKRAD_DB_URL (postgres://…@postgres:5432/…).
	DBURL string
	// RedisURL is HIKRAD_REDIS_URL (redis://redis:6379/0).
	RedisURL string
	// EncryptionKey is HIKRAD_ENCRYPTION_KEY decoded: exactly 32 bytes,
	// the AES-256-GCM key for reversible subscriber RADIUS passwords
	// (NFR-4.2). Only the authorize path may decrypt with it.
	EncryptionKey []byte
	// JWTSecret is HIKRAD_JWT_SECRET, the HMAC secret for panel/portal tokens.
	JWTSecret string
	// Env is HIKRAD_ENV: "dev" or "prod". Dev-only behavior (the C7 auth
	// stub, seed shortcuts) must gate on IsDev.
	Env string
	// MigrationsDir is where the boot-time runner finds SQL migrations.
	// HIKRAD_MIGRATIONS_DIR; defaults to "migrations" (the image sets
	// /app/migrations).
	MigrationsDir string
}

// IsDev reports whether dev-only behavior (contract C7 auth stub) is enabled.
func (c Config) IsDev() bool { return c.Env == "dev" }

// LoadConfig reads and validates the HIKRAD_* environment. It fails fast and
// names every missing variable so a broken .env is diagnosable in one pass.
func LoadConfig() (Config, error) {
	var cfg Config
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
	keyB64 := need("HIKRAD_ENCRYPTION_KEY")
	cfg.JWTSecret = need("HIKRAD_JWT_SECRET")
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("platform: missing required environment variables: %s (generate with scripts/gen-env.sh)", strings.Join(missing, ", "))
	}

	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return Config{}, fmt.Errorf("platform: HIKRAD_ENCRYPTION_KEY is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("platform: HIKRAD_ENCRYPTION_KEY must decode to 32 bytes (AES-256), got %d", len(key))
	}
	cfg.EncryptionKey = key

	switch env := os.Getenv("HIKRAD_ENV"); env {
	case "":
		// Unset means production: dev conveniences must be opted into.
		cfg.Env = "prod"
	case "dev", "prod":
		cfg.Env = env
	default:
		return Config{}, fmt.Errorf("platform: HIKRAD_ENV must be \"dev\" or \"prod\", got %q", env)
	}

	if cfg.MigrationsDir = os.Getenv("HIKRAD_MIGRATIONS_DIR"); cfg.MigrationsDir == "" {
		cfg.MigrationsDir = "migrations"
	}

	return cfg, nil
}
