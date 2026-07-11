package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// config is hikrad-monitor's environment. DB + Redis are shared with the C5
// contract; the encryption key is optional (needed only to decrypt SNMP
// communities). No JWT secret: this binary serves no HTTP and issues no tokens.
type config struct {
	DBURL         string
	RedisURL      string
	EncryptionKey []byte
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
		return config{}, fmt.Errorf("hikrad-monitor: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	if raw := os.Getenv("HIKRAD_ENCRYPTION_KEY"); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return config{}, fmt.Errorf("hikrad-monitor: HIKRAD_ENCRYPTION_KEY is not valid base64: %w", err)
		}
		cfg.EncryptionKey = key
	}
	return cfg, nil
}
