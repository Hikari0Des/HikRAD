package updates_test

// DB-backed suite for the updates relay module (gated on HIKRAD_TEST_DB_URL,
// matching the repo pattern — see internal/billing/db_test.go). Covers gate
// item 6 (docs/v2/phases/phase-v2-7-one-click-update/00-phase.md): the
// system.update permission gate. It deliberately does NOT set
// HIKRAD_UPDATER_TOKEN, so every handler's own configured() check reports
// "not provisioned" (503) rather than trying to reach a real socket — that
// keeps this suite focused on the ONE thing it's testing (auth.Require ran
// and decided correctly), not on socket plumbing (covered by
// backend/cmd/hikrad-updaterd's own tests instead). The assertion is
// therefore "not 403" for an allowed caller and "403" for a denied one, not
// a specific 2xx body.

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

	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/updates"
)

type env struct {
	srv *httptest.Server
	db  *pgxpool.Pool
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
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping updates DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "updates-db-test-secret")
	t.Setenv("HIKRAD_ENV", "dev")
	// Deliberately unset: see the package doc comment above.
	os.Unsetenv("HIKRAD_UPDATER_TOKEN")

	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

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

	return env{srv: srv, db: db}
}

func (e env) seedManager(t *testing.T, role string) (username string) {
	t.Helper()
	ctx := context.Background()
	username = uniq(role + "_")
	bh, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ($1,$2,$3)`,
		username, string(bh), role); err != nil {
		t.Fatalf("seed manager: %v", err)
	}
	return username
}

func (e env) grantOverride(t *testing.T, username, permission string) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.db.Exec(ctx,
		`INSERT INTO manager_permission_overrides (manager_id, permission, granted)
		 SELECT id, $2, true FROM managers WHERE username = $1`,
		username, permission); err != nil {
		t.Fatalf("grant override: %v", err)
	}
}

func (e env) login(t *testing.T, username string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": "password123"})
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d: %s", username, res.StatusCode, raw)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	return out.AccessToken
}

func (e env) request(t *testing.T, method, path, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

var routes = []struct {
	method string
	path   string
}{
	{"GET", "/api/v1/system/update/check"},
	{"POST", "/api/v1/system/update"},
	{"GET", "/api/v1/system/update/status"},
	// The SSE stream route is exercised separately (it never returns while
	// polling); its own permission gate is identical middleware wiring to
	// the other three, proven by module.go using the same auth.Require call
	// for all four routes — a dedicated 403 check on it alone would be
	// redundant with these three, not additional coverage.
}

func TestPermissionGate_Denied(t *testing.T) {
	e := setup(t)
	username := e.seedManager(t, "operator") // no system.update in rolePermissions
	token := e.login(t, username)

	for _, rt := range routes {
		res := e.request(t, rt.method, rt.path, token)
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with operator token: status = %d, want 403", rt.method, rt.path, res.StatusCode)
		}
	}

	// No token at all: unauthorized, not forbidden — a different failure
	// mode, worth distinguishing so a future regression that merges the two
	// codes is caught.
	res := e.request(t, "GET", "/api/v1/system/update/check", "")
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", res.StatusCode)
	}
}

func TestPermissionGate_Allowed(t *testing.T) {
	e := setup(t)

	adminUser := e.seedManager(t, "admin")
	adminToken := e.login(t, adminUser)

	// A non-admin role granted the permission explicitly through the
	// existing override mechanism (proving it's a normal permission string,
	// not special-cased to admin only, C5).
	grantedUser := e.seedManager(t, "operator")
	e.grantOverride(t, grantedUser, "system.update")
	grantedToken := e.login(t, grantedUser)

	for _, tok := range []struct {
		name  string
		token string
	}{{"admin (wildcard)", adminToken}, {"operator with explicit override", grantedToken}} {
		for _, rt := range routes {
			res := e.request(t, rt.method, rt.path, tok.token)
			res.Body.Close()
			if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized {
				t.Errorf("%s: %s %s = %d, want anything but 401/403 (daemon not configured in this suite, that's fine)",
					tok.name, rt.method, rt.path, res.StatusCode)
			}
		}
	}
}
