package auth

// DB-backed suite for the auth module (gated on HIKRAD_TEST_DB_URL, matching
// the repo pattern). It boots the real router against Postgres (+ Redis if
// HIKRAD_TEST_REDIS_URL is set) and exercises login, the argon2id upgrade
// path, refresh rotation + reuse detection, permission denial + audit,
// panel-session revocation, audit immutability, and lockout.
//
// Every test is self-scoped with unique usernames and filters audit assertions
// by the actor/entity it created, so the suite is safe to run in parallel with
// the cmd/hikrad-api integration test on the same shared CI database (it never
// truncates shared tables).

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
	"strconv"
	"strings"
	"testing"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

const testJWTSecret = "auth-db-test-secret"

func uniq(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

type env struct {
	srv       *httptest.Server
	db        *pgxpool.Pool
	adminUser string
	adminPass string
	// xff is a unique X-Forwarded-For per test: clientIP() prefers it (Caddy
	// sets it in prod), so per-IP rate-limit counters don't bleed between
	// tests that otherwise all originate from 127.0.0.1 on a shared Redis.
	xff string
}

func uniqIP() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "10." + strconv.Itoa(int(b[0])) + "." + strconv.Itoa(int(b[1])) + "." + strconv.Itoa(int(b[2])+1)
}

func setupRouter(t *testing.T) env {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping auth DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", testJWTSecret)
	t.Setenv("HIKRAD_ENV", "dev")

	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	// Seed a UNIQUE admin the Phase-1 way (bcrypt) so the argon2id upgrade path
	// is exercised without clobbering another test's "admin".
	adminUser := uniq("admin")
	bh, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	if _, err := db.Exec(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ($1, $2, 'admin')`,
		adminUser, string(bh)); err != nil {
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
	return env{srv: srv, db: db, adminUser: adminUser, adminPass: "admin", xff: uniqIP()}
}

type apiResp struct {
	status int
	body   []byte
}

func (a apiResp) json(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(a.body, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", a.body, err)
	}
}

func call(t *testing.T, e env, method, path, token string, body any) apiResp {
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
	if e.xff != "" {
		req.Header.Set("X-Forwarded-For", e.xff)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return apiResp{status: resp.StatusCode, body: raw}
}

type loginOut struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Manager      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	} `json:"manager"`
}

func login(t *testing.T, e env, user, pass string) loginOut {
	t.Helper()
	r := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": pass})
	if r.status != http.StatusOK {
		t.Fatalf("login %s = %d: %s", user, r.status, r.body)
	}
	var out loginOut
	r.json(t, &out)
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatalf("login missing tokens: %s", r.body)
	}
	return out
}

func loginAdmin(t *testing.T, e env) loginOut { return login(t, e, e.adminUser, e.adminPass) }

// auditCountByActor counts audit rows for a specific actor + action, keeping
// assertions independent of other tests sharing the DB.
func auditCountByActor(t *testing.T, db *pgxpool.Pool, actorID, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE actor_id = $1::uuid AND action = $2`,
		actorID, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// auditCountByEntity counts rows for an entity + action, used where the actor
// is (correctly) null — e.g. a failed login has no authenticated actor.
func auditCountByEntity(t *testing.T, db *pgxpool.Pool, entityType, entityID, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity_type = $1 AND entity_id = $2 AND action = $3`,
		entityType, entityID, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLoginUpgradesBcryptAndAudits(t *testing.T) {
	e := setupRouter(t)
	out := loginAdmin(t, e)
	if out.Manager.Role != "admin" || out.Manager.Username != e.adminUser {
		t.Fatalf("manager = %+v", out.Manager)
	}
	var hash string
	if err := e.db.QueryRow(context.Background(),
		`SELECT password_hash FROM managers WHERE username=$1`, e.adminUser).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("password not upgraded to argon2id: %s", hash)
	}
	loginAdmin(t, e) // still works against the upgraded hash
	if auditCountByActor(t, e.db, out.Manager.ID, "auth.login") < 2 {
		t.Fatal("auth.login not audited")
	}
}

func TestLoginWrongPasswordAudited(t *testing.T) {
	e := setupRouter(t)
	// Discover the admin id via a good login first.
	admin := loginAdmin(t, e)
	r := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": e.adminUser, "password": "nope"})
	if r.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", r.status, r.body)
	}
	if !strings.Contains(string(r.body), "invalid_credentials") {
		t.Fatalf("body = %s", r.body)
	}
	if auditCountByEntity(t, e.db, "manager", admin.Manager.ID, "auth.login_failed") < 1 {
		t.Fatal("failed login not audited")
	}
}

func TestAuthenticatedRouteAcceptsRealToken(t *testing.T) {
	e := setupRouter(t)
	out := loginAdmin(t, e)
	// Use an auth-owned protected route (/api/v1/managers, managers.view) as the
	// authn-seam probe. In Phase 2 every domain module imports internal/auth for
	// the middleware, so this internal (package auth) test can no longer
	// blank-import a domain package like subscribers without an import cycle;
	// auth's own guarded route proves the real authenticator was installed.
	r := call(t, e, "GET", "/api/v1/managers", out.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("managers list = %d: %s", r.status, r.body)
	}
	if r := call(t, e, "GET", "/api/v1/managers", "", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d, want 401", r.status)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	e := setupRouter(t)
	out := loginAdmin(t, e)

	r := call(t, e, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": out.RefreshToken})
	if r.status != http.StatusOK {
		t.Fatalf("refresh = %d: %s", r.status, r.body)
	}
	var rotated loginOut
	r.json(t, &rotated)
	if rotated.RefreshToken == out.RefreshToken {
		t.Fatal("refresh token did not rotate")
	}

	// Reuse of the rotated-away token → 401 + revoke the whole chain.
	r = call(t, e, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": out.RefreshToken})
	if r.status != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want 401: %s", r.status, r.body)
	}
	if auditCountByActor(t, e.db, out.Manager.ID, "auth.refresh_reuse") < 1 {
		t.Fatal("refresh reuse not audited")
	}
	// Even the freshly-rotated token now fails (session revoked).
	r = call(t, e, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": rotated.RefreshToken})
	if r.status != http.StatusUnauthorized {
		t.Fatalf("post-revoke refresh = %d, want 401: %s", r.status, r.body)
	}
}

func TestPermissionDenialAudited(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)

	sara := uniq("sara")
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": sara, "password": "operator-pw", "role": "operator",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create operator = %d: %s", r.status, r.body)
	}
	op := login(t, e, sara, "operator-pw")

	r = call(t, e, "GET", "/api/v1/managers", op.AccessToken, nil)
	if r.status != http.StatusForbidden {
		t.Fatalf("operator GET managers = %d, want 403: %s", r.status, r.body)
	}
	if auditCountByActor(t, e.db, op.Manager.ID, "auth.denied") < 1 {
		t.Fatal("permission denial not audited")
	}
	if r := call(t, e, "GET", "/api/v1/managers", admin.AccessToken, nil); r.status != http.StatusOK {
		t.Fatalf("admin GET managers = %d: %s", r.status, r.body)
	}
}

func TestPasswordResetRevokesOtherSessions(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	omar := uniq("omar")
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": omar, "password": "omar-pass-1", "role": "operator",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create = %d: %s", r.status, r.body)
	}
	var created managerView
	r.json(t, &created)
	s1 := login(t, e, omar, "omar-pass-1")
	s2 := login(t, e, omar, "omar-pass-1")

	r = call(t, e, "PUT", "/api/v1/managers/"+created.ID, admin.AccessToken, map[string]any{
		"password": "omar-pass-2",
	})
	if r.status != http.StatusOK {
		t.Fatalf("reset = %d: %s", r.status, r.body)
	}
	for i, s := range []loginOut{s1, s2} {
		rr := call(t, e, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": s.RefreshToken})
		if rr.status != http.StatusUnauthorized {
			t.Fatalf("session %d refresh after reset = %d, want 401", i+1, rr.status)
		}
	}
	if rr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": omar, "password": "omar-pass-1"}); rr.status != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", rr.status)
	}
	login(t, e, omar, "omar-pass-2")
}

func TestPanelSessionListAndRevoke(t *testing.T) {
	e := setupRouter(t)
	out := loginAdmin(t, e)

	r := call(t, e, "GET", "/api/v1/panel-sessions", out.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("list sessions = %d: %s", r.status, r.body)
	}
	var list struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	r.json(t, &list)
	var current string
	for _, it := range list.Items {
		if it.Current {
			current = it.ID
		}
	}
	if current == "" {
		t.Fatalf("no current session in %s", r.body)
	}
	if rr := call(t, e, "DELETE", "/api/v1/panel-sessions/"+current, out.AccessToken, nil); rr.status != http.StatusNoContent {
		t.Fatalf("revoke = %d: %s", rr.status, rr.body)
	}
	rr := call(t, e, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": out.RefreshToken})
	if rr.status != http.StatusUnauthorized {
		t.Fatalf("refresh after self-revoke = %d, want 401", rr.status)
	}
}

func TestAuditLogIsImmutable(t *testing.T) {
	e := setupRouter(t)
	out := loginAdmin(t, e) // guarantees at least our own audit row exists
	ctx := context.Background()
	var id int64
	if err := e.db.QueryRow(ctx,
		`SELECT id FROM audit_log WHERE actor_id = $1::uuid ORDER BY id LIMIT 1`,
		out.Manager.ID).Scan(&id); err != nil {
		t.Fatalf("need an audit row: %v", err)
	}
	if _, err := e.db.Exec(ctx, `UPDATE audit_log SET action = 'tampered' WHERE id = $1`, id); err == nil {
		t.Fatal("UPDATE on audit_log must be refused at the DB level (AC-28c)")
	}
	if _, err := e.db.Exec(ctx, `DELETE FROM audit_log WHERE id = $1`, id); err == nil {
		t.Fatal("DELETE on audit_log must be refused at the DB level (AC-28c)")
	}
}

func TestAuditLogEndpointFiltersByEntity(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	noor := uniq("noor")
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": noor, "password": "noor-pass-1", "role": "agent", "scoped": true,
	})
	var created managerView
	r.json(t, &created)

	r = call(t, e, "GET", "/api/v1/audit-log?entity_type=manager&entity_id="+created.ID, admin.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("audit-log = %d: %s", r.status, r.body)
	}
	var list struct {
		Items []struct {
			Action   string `json:"action"`
			EntityID string `json:"entity_id"`
		} `json:"items"`
	}
	r.json(t, &list)
	found := false
	for _, it := range list.Items {
		if it.EntityID != created.ID {
			t.Fatalf("filter leaked entity %s", it.EntityID)
		}
		if it.Action == "managers.create" {
			found = true
		}
	}
	if !found {
		t.Fatalf("managers.create not in audit trail: %s", r.body)
	}
}

func TestLockoutAfterFailuresAndAdminUnlock(t *testing.T) {
	if os.Getenv("HIKRAD_TEST_REDIS_URL") == "" {
		t.Skip("HIKRAD_TEST_REDIS_URL not set; lockout needs Redis")
	}
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	victim := uniq("victim")
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": victim, "password": "victim-pass-1", "role": "operator",
	})
	var vrow managerView
	r.json(t, &vrow)

	for i := 0; i < accountFailLimit; i++ {
		call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": victim, "password": "wrong"})
	}
	rr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": victim, "password": "victim-pass-1"})
	if rr.status != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after lockout, got %d: %s", rr.status, rr.body)
	}
	if u := call(t, e, "POST", "/api/v1/managers/"+vrow.ID+"/unlock", admin.AccessToken, nil); u.status != http.StatusNoContent {
		t.Fatalf("unlock = %d: %s", u.status, u.body)
	}
	login(t, e, victim, "victim-pass-1")
}
