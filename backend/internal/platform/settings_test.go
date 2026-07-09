package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeDB implements settingsDB over an in-memory map so the cache behavior
// is observable (query counting) without Postgres.
type fakeDB struct {
	mu      sync.Mutex
	store   map[string][]byte
	queries atomic.Int64
	execs   atomic.Int64
}

func newFakeDB() *fakeDB { return &fakeDB{store: make(map[string][]byte)} }

func (f *fakeDB) put(key string, v any) {
	raw, _ := json.Marshal(v)
	f.mu.Lock()
	f.store[key] = raw
	f.mu.Unlock()
}

func (f *fakeDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	f.execs.Add(1)
	key, ok := args[0].(string)
	if !ok {
		return pgconn.CommandTag{}, fmt.Errorf("fakeDB: arg 0 is %T, want string", args[0])
	}
	raw, ok := args[1].([]byte)
	if !ok {
		return pgconn.CommandTag{}, fmt.Errorf("fakeDB: arg 1 is %T, want []byte", args[1])
	}
	f.mu.Lock()
	f.store[key] = append([]byte(nil), raw...)
	f.mu.Unlock()
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	f.queries.Add(1)
	key := args[0].(string)
	f.mu.Lock()
	raw, ok := f.store[key]
	f.mu.Unlock()
	if !ok {
		return fakeRow{err: pgx.ErrNoRows}
	}
	return fakeRow{value: append([]byte(nil), raw...)}
}

type fakeRow struct {
	value []byte
	err   error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	p, ok := dest[0].(*[]byte)
	if !ok {
		return fmt.Errorf("fakeRow: dest 0 is %T, want *[]byte", dest[0])
	}
	*p = r.value
	return nil
}

func TestSettingsGetTyped(t *testing.T) {
	db := newFakeDB()
	db.put("locale.timezone", "Asia/Baghdad")
	db.put("locale.currency", "IQD")
	db.put("notifications.smtp", map[string]any{})
	s := NewSettings(db)
	ctx := context.Background()

	tz, err := Get[string](ctx, s, "locale.timezone")
	if err != nil {
		t.Fatalf("Get locale.timezone: %v", err)
	}
	if tz != "Asia/Baghdad" {
		t.Errorf("locale.timezone = %q, want Asia/Baghdad", tz)
	}

	smtp, err := Get[map[string]any](ctx, s, "notifications.smtp")
	if err != nil {
		t.Fatalf("Get notifications.smtp: %v", err)
	}
	if len(smtp) != 0 {
		t.Errorf("notifications.smtp = %v, want empty group", smtp)
	}

	if _, err := Get[int](ctx, s, "locale.currency"); err == nil {
		t.Error("decoding a string setting into int should fail")
	}
}

func TestSettingsGetMissing(t *testing.T) {
	s := NewSettings(newFakeDB())
	_, err := Get[string](context.Background(), s, "nope")
	if !errors.Is(err, ErrSettingNotFound) {
		t.Fatalf("err = %v, want ErrSettingNotFound", err)
	}
}

func TestSettingsGetCaches(t *testing.T) {
	db := newFakeDB()
	db.put("locale.currency", "IQD")
	s := NewSettings(db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if v, err := Get[string](ctx, s, "locale.currency"); err != nil || v != "IQD" {
			t.Fatalf("read %d: v=%q err=%v", i, v, err)
		}
	}
	if n := db.queries.Load(); n != 1 {
		t.Errorf("queries = %d, want 1 (cache should serve repeat reads)", n)
	}

	// A change behind the service's back stays invisible until invalidated —
	// that is the cache contract.
	db.put("locale.currency", "USD")
	if v, _ := Get[string](ctx, s, "locale.currency"); v != "IQD" {
		t.Errorf("cached read = %q, want IQD", v)
	}
}

func TestSettingsInvalidate(t *testing.T) {
	db := newFakeDB()
	db.put("locale.currency", "IQD")
	s := NewSettings(db)
	ctx := context.Background()

	if _, err := Get[string](ctx, s, "locale.currency"); err != nil {
		t.Fatal(err)
	}
	db.put("locale.currency", "USD")
	s.Invalidate("locale.currency")

	v, err := Get[string](ctx, s, "locale.currency")
	if err != nil {
		t.Fatal(err)
	}
	if v != "USD" {
		t.Errorf("post-invalidation read = %q, want USD (must re-query)", v)
	}
	if n := db.queries.Load(); n != 2 {
		t.Errorf("queries = %d, want 2", n)
	}
}

func TestSettingsSetUpdatesStoreCacheAndHooks(t *testing.T) {
	db := newFakeDB()
	s := NewSettings(db)
	ctx := context.Background()

	var changed []string
	s.OnChange(func(key string) { changed = append(changed, key) })

	if err := s.Set(ctx, "locale.timezone", "Asia/Baghdad"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if db.execs.Load() != 1 {
		t.Errorf("execs = %d, want 1 (upsert must hit the DB)", db.execs.Load())
	}
	if len(changed) != 1 || changed[0] != "locale.timezone" {
		t.Errorf("OnChange hook got %v, want [locale.timezone]", changed)
	}

	// The Set must have warmed the cache: no query needed to read it back.
	v, err := Get[string](ctx, s, "locale.timezone")
	if err != nil || v != "Asia/Baghdad" {
		t.Fatalf("read-back v=%q err=%v", v, err)
	}
	if n := db.queries.Load(); n != 0 {
		t.Errorf("queries = %d, want 0 (Set should warm the cache)", n)
	}
}

func TestSettingsSetUnencodableValue(t *testing.T) {
	s := NewSettings(newFakeDB())
	if err := s.Set(context.Background(), "bad", func() {}); err == nil {
		t.Fatal("Set accepted an unmarshalable value")
	}
}

// TestSettingsConcurrentAccess exercises the cache under parallel Get/Set/
// Invalidate; run with -race (CI does) to prove concurrency safety.
func TestSettingsConcurrentAccess(t *testing.T) {
	db := newFakeDB()
	db.put("locale.timezone", "Asia/Baghdad")
	s := NewSettings(db)
	s.OnChange(func(string) {})
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				switch j % 3 {
				case 0:
					if _, err := Get[string](ctx, s, "locale.timezone"); err != nil {
						t.Errorf("Get: %v", err)
						return
					}
				case 1:
					if err := s.Set(ctx, "locale.timezone", fmt.Sprintf("tz-%d-%d", n, j)); err != nil {
						t.Errorf("Set: %v", err)
						return
					}
				case 2:
					s.Invalidate("locale.timezone")
				}
			}
		}(i)
	}
	wg.Wait()
}
