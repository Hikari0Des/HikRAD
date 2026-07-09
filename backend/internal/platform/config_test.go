package platform

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// validEnv sets a complete, valid C5 environment and returns the raw key bytes.
func validEnv(t *testing.T) []byte {
	t.Helper()
	key := bytes.Repeat([]byte{0xAB}, 32)
	t.Setenv("HIKRAD_DB_URL", "postgres://hikrad:pw@postgres:5432/hikrad?sslmode=disable")
	t.Setenv("HIKRAD_REDIS_URL", "redis://redis:6379/0")
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "test-jwt-secret")
	t.Setenv("HIKRAD_ENV", "dev")
	t.Setenv("HIKRAD_MIGRATIONS_DIR", "")
	return key
}

func TestLoadConfigValid(t *testing.T) {
	key := validEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DBURL != "postgres://hikrad:pw@postgres:5432/hikrad?sslmode=disable" {
		t.Errorf("DBURL = %q", cfg.DBURL)
	}
	if cfg.RedisURL != "redis://redis:6379/0" {
		t.Errorf("RedisURL = %q", cfg.RedisURL)
	}
	if !bytes.Equal(cfg.EncryptionKey, key) {
		t.Error("EncryptionKey does not round-trip through base64")
	}
	if cfg.JWTSecret != "test-jwt-secret" {
		t.Errorf("JWTSecret = %q", cfg.JWTSecret)
	}
	if !cfg.IsDev() {
		t.Error("IsDev() = false for HIKRAD_ENV=dev")
	}
	if cfg.MigrationsDir != "migrations" {
		t.Errorf("MigrationsDir default = %q, want migrations", cfg.MigrationsDir)
	}
}

func TestLoadConfigMissingVarsAreAllNamed(t *testing.T) {
	validEnv(t)
	t.Setenv("HIKRAD_DB_URL", "")
	t.Setenv("HIKRAD_JWT_SECRET", "")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig succeeded with missing variables")
	}
	for _, name := range []string{"HIKRAD_DB_URL", "HIKRAD_JWT_SECRET"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name missing %s", err, name)
		}
	}
}

func TestLoadConfigEncryptionKeyValidation(t *testing.T) {
	cases := []struct {
		name, value string
	}{
		{"not base64", "%%%not-base64%%%"},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("short"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			t.Setenv("HIKRAD_ENCRYPTION_KEY", tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("LoadConfig accepted an invalid HIKRAD_ENCRYPTION_KEY")
			}
		})
	}
}

func TestLoadConfigEnvValues(t *testing.T) {
	validEnv(t)

	t.Setenv("HIKRAD_ENV", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig with unset HIKRAD_ENV: %v", err)
	}
	if cfg.Env != "prod" || cfg.IsDev() {
		t.Errorf("unset HIKRAD_ENV must default to prod, got %q", cfg.Env)
	}

	t.Setenv("HIKRAD_ENV", "staging")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig accepted HIKRAD_ENV=staging")
	}
}

func TestLoadConfigMigrationsDirOverride(t *testing.T) {
	validEnv(t)
	t.Setenv("HIKRAD_MIGRATIONS_DIR", "/app/migrations")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MigrationsDir != "/app/migrations" {
		t.Errorf("MigrationsDir = %q, want /app/migrations", cfg.MigrationsDir)
	}
}
