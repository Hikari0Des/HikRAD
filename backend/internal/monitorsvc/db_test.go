package monitorsvc_test

// DB-backed suite harness for internal/monitorsvc (v2-10, gated on
// HIKRAD_TEST_DB_URL, matching the repo pattern in internal/auth/subscribers/
// importer). internal/monitorsvc had no pre-existing DB-gated HTTP-endpoint
// suite before this phase — every prior test in this package is unit-level
// (quiet hours, cooldown, dispatcher, SNMP encoding, downtime detection,
// WhatsApp templates). This harness follows the exact env/call/setup shape
// those three packages already use.

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
	_ "github.com/hikrad/hikrad/internal/billing"
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/monitorsvc"
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/radius"
	_ "github.com/hikrad/hikrad/internal/subscribers"
)

type env struct {
	srv        *httptest.Server
	db         *pgxpool.Pool
	adminToken string
}

func uniq(p string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return p + hex.EncodeToString(b)
}

func setup(t *testing.T) env {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping monitorsvc DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 41)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "monitorsvc-db-test-secret")
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
	if _, err := db.Exec(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ($1,$2,'admin')`,
		adminUser, string(bh)); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	var rdb *redis.Client
	if os.Getenv("HIKRAD_TEST_REDIS_URL") != "" {
		if rdb, err = platform.NewRedis(ctx, platform.Config{RedisURL: redisURL}); err != nil {
			t.Fatalf("redis: %v", err)
		}
		t.Cleanup(func() { _ = rdb.Close() })
	}

	deps := httpapi.Deps{DB: db, Redis: rdb, Settings: platform.NewSettings(db), Log: log}
	srv := httptest.NewServer(httpapi.NewRouter(deps, true))
	t.Cleanup(srv.Close)

	e := env{srv: srv, db: db}
	e.adminToken = e.login(t, adminUser, "admin")
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

func (e env) do(t *testing.T, method, path, token string, body any) resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: raw}
}

func (e env) login(t *testing.T, user, pass string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": pass})
	if r.status != http.StatusOK {
		t.Fatalf("login %s = %d: %s", user, r.status, r.body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	r.into(t, &out)
	return out.AccessToken
}

// createManager creates a manager with the given builtin role (admin token
// required) and returns its id + a logged-in access token.
func (e env) createManager(t *testing.T, role string, scoped bool) (id, token string) {
	t.Helper()
	username := uniq(role + "_")
	password := "pass-1234"
	r := e.do(t, "POST", "/api/v1/managers", e.adminToken, map[string]any{
		"username": username, "password": password, "role": role, "scoped": scoped,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create %s manager = %d: %s", role, r.status, r.body)
	}
	var created struct {
		ID string `json:"id"`
	}
	r.into(t, &created)
	return created.ID, e.login(t, username, password)
}
