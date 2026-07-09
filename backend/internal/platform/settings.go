package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSettingNotFound is returned when a key has no row in the settings table.
// The v1 defaults (locale.timezone, locale.currency, notification groups) are
// seeded by migration 0010, so callers normally only see this for optional
// keys.
var ErrSettingNotFound = errors.New("platform: setting not found")

// Settings is the typed settings service over the C6 `settings` table
// (FR-53.1): JSONB values with an in-process, concurrency-safe cache and an
// invalidation hook. Typed reads go through the package-level generic
// Get[T] (Go interfaces cannot carry generic methods, so the contract's
// "Get[T](key)" lives at package level over GetRaw).
//
// It is an interface so C3's Deps can hold it by value and tests can fake it.
type Settings interface {
	// GetRaw returns the raw JSON value for key, from cache when warm.
	GetRaw(ctx context.Context, key string) (json.RawMessage, error)
	// Set marshals v, upserts the row, refreshes the cache, and fires every
	// registered OnChange hook with the key.
	Set(ctx context.Context, key string, v any) error
	// Invalidate drops key from the cache so the next read hits the DB —
	// the hook point for cross-process invalidation in later phases.
	Invalidate(key string)
	// OnChange registers fn to run (synchronously) after every successful Set.
	OnChange(fn func(key string))
}

// Get returns the setting for key decoded into T, e.g.
//
//	tz, err := platform.Get[string](ctx, settings, "locale.timezone")
func Get[T any](ctx context.Context, s Settings, key string) (T, error) {
	var v T
	raw, err := s.GetRaw(ctx, key)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("platform: setting %q: decode into %T: %w", key, v, err)
	}
	return v, nil
}

// settingsDB is the slice of *pgxpool.Pool the service needs (faked in tests).
type settingsDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewSettings builds the pgx-backed Settings service. The cache warms lazily
// per key; pass Deps.DB (a *pgxpool.Pool) as db.
func NewSettings(db settingsDB) Settings {
	return &settingsService{
		db:    db,
		cache: make(map[string]json.RawMessage),
	}
}

type settingsService struct {
	db    settingsDB
	mu    sync.RWMutex
	cache map[string]json.RawMessage
	hooks []func(key string)
}

func (s *settingsService) GetRaw(ctx context.Context, key string) (json.RawMessage, error) {
	s.mu.RLock()
	cached, ok := s.cache[key]
	s.mu.RUnlock()
	if ok {
		return cached, nil
	}

	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %q", ErrSettingNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("platform: read setting %q: %w", key, err)
	}

	val := json.RawMessage(append([]byte(nil), raw...))
	s.mu.Lock()
	s.cache[key] = val
	s.mu.Unlock()
	return val, nil
}

func (s *settingsService) Set(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("platform: setting %q: encode: %w", key, err)
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, raw)
	if err != nil {
		return fmt.Errorf("platform: write setting %q: %w", key, err)
	}

	s.mu.Lock()
	s.cache[key] = json.RawMessage(raw)
	hooks := append([]func(string){}, s.hooks...)
	s.mu.Unlock()

	// Hooks run outside the lock so a hook may safely call back into the
	// service (e.g. re-read a sibling key).
	for _, fn := range hooks {
		fn(key)
	}
	return nil
}

func (s *settingsService) Invalidate(key string) {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
}

func (s *settingsService) OnChange(fn func(key string)) {
	s.mu.Lock()
	s.hooks = append(s.hooks, fn)
	s.mu.Unlock()
}
