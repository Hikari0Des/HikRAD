package subscribers_test

// DB-backed suite harness (gated on HIKRAD_TEST_DB_URL, matching the repo
// pattern in internal/auth). It boots the full router against Postgres/Timescale
// so the subscribers module runs with its real dependencies: auth middleware,
// audit log, crypto, the radius policy seam, and (optionally) Redis for cache
// invalidation. Each test self-scopes with unique usernames so the suite is safe
// on a shared CI database.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	// Register every module the router mounts (their init() calls httpapi.Add).
	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/radius"
	_ "github.com/hikrad/hikrad/internal/subscribers"
)

type testEnv struct {
	srv     *httptest.Server
	db      *pgxpool.Pool
	rdb     *redis.Client
	token   string
	adminID string
	prefix  string // unique per test run for row isolation
}

func uniq(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func setup(t *testing.T) testEnv {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping subscribers DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "subscribers-db-test-secret")
	t.Setenv("HIKRAD_ENV", "dev")

	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	adminUser := uniq("admin_")
	bh, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	var adminID string
	if err := db.QueryRow(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ($1,$2,'admin') RETURNING id::text`,
		adminUser, string(bh)).Scan(&adminID); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	var rdb *redis.Client
	if os.Getenv("HIKRAD_TEST_REDIS_URL") != "" {
		rdb, err = platform.NewRedis(ctx, platform.Config{RedisURL: redisURL})
		if err != nil {
			t.Fatalf("redis: %v", err)
		}
		t.Cleanup(func() { _ = rdb.Close() })
	}

	deps := httpapi.Deps{DB: db, Redis: rdb, Settings: platform.NewSettings(db), Log: log}
	srv := httptest.NewServer(httpapi.NewRouter(deps, true))
	t.Cleanup(srv.Close)

	e := testEnv{srv: srv, db: db, rdb: rdb, adminID: adminID, prefix: uniq("s_")}
	e.token = e.login(t, adminUser, "admin")
	return e
}

type resp struct {
	status int
	body   []byte
}

func (r resp) into(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.body, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", r.body, err)
	}
}

func (e testEnv) do(t *testing.T, method, path string, body any) resp {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: raw}
}

func (e testEnv) login(t *testing.T, user, pass string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": user, "password": pass})
	if r.status != http.StatusOK {
		t.Fatalf("login %s = %d: %s", user, r.status, r.body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	r.into(t, &out)
	if out.AccessToken == "" {
		t.Fatalf("no access token: %s", r.body)
	}
	return out.AccessToken
}

// createProfile inserts a minimal active profile and returns its id.
func (e testEnv) createProfile(t *testing.T, name string, down, up int) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles", map[string]any{
		"name": name, "price_iqd": 10000, "duration_days": 30,
		"rate_down_kbps": down, "rate_up_kbps": up,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create profile = %d: %s", r.status, r.body)
	}
	var p struct {
		ID string `json:"id"`
	}
	r.into(t, &p)
	return p.ID
}

// auditCount counts audit rows for an entity + action (actor-independent so it
// is robust on a shared DB).
func (e testEnv) auditCount(t *testing.T, action, entityID string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = $1 AND entity_id = $2`,
		action, entityID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
