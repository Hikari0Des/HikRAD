package platform

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5:// database driver
	_ "github.com/golang-migrate/migrate/v4/source/file"     // file:// source driver
)

// Migrate applies all pending SQL migrations from dir against dbURL. It runs
// on every hikrad-api boot before serving (FR-51.4: migrations are
// forward-only; there is no down path in production).
//
// File naming: NNNN_slug.up.sql in backend/migrations/, where NNNN comes from
// the per-agent range assigned in the phase brief (contract C1/C6; the
// .up.sql suffix is required by golang-migrate, C9's frozen tool).
func Migrate(dbURL, dir string, log *slog.Logger) error {
	// golang-migrate picks its database driver from the URL scheme; route the
	// standard postgres:// URL (contract C5) to the pgx/v5 driver.
	url := dbURL
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(url, scheme) {
			url = "pgx5://" + strings.TrimPrefix(url, scheme)
			break
		}
	}

	m, err := migrate.New("file://"+filepath.ToSlash(dir), url)
	if err != nil {
		return fmt.Errorf("platform: open migrations %q: %w", dir, err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Warn("closing migration source", "err", srcErr)
		}
		if dbErr != nil {
			log.Warn("closing migration db connection", "err", dbErr)
		}
	}()

	switch err := m.Up(); {
	case err == nil:
		version, dirty, vErr := m.Version()
		if vErr != nil {
			return fmt.Errorf("platform: read migration version: %w", vErr)
		}
		log.Info("migrations applied", "version", version, "dirty", dirty)
	case errors.Is(err, migrate.ErrNoChange):
		log.Info("migrations already up to date")
	default:
		return fmt.Errorf("platform: run migrations: %w", err)
	}
	return nil
}
