package profiles_test

// DB-backed profiles bulk-endpoint suite (FR-111.2, v3-6). Gated on
// HIKRAD_TEST_DB_URL. Boots the full router so the async job, per-action
// permission enforcement, and the FR-7.4 in-use guard are exercised end to end.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/radius"
	_ "github.com/hikrad/hikrad/internal/subscribers"
)

type benv struct {
	srv   *httptest.Server
	db    *pgxpool.Pool
	token string
}

func buniq(p string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return p + hex.EncodeToString(b)
}

func bsetup(t *testing.T) benv {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping profiles bulk DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 23)
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", "redis://unused:6379/0")
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "profiles-bulk-test-secret")
	t.Setenv("HIKRAD_ENV", "dev")

	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	adminUser := buniq("admin_")
	bh, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	if _, err := db.Exec(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ($1,$2,'admin')`,
		adminUser, string(bh)); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	deps := httpapi.Deps{DB: db, Redis: nil, Settings: platform.NewSettings(db), Log: log}
	srv := httptest.NewServer(httpapi.NewRouter(deps, true))
	t.Cleanup(srv.Close)

	e := benv{srv: srv, db: db}
	e.token = e.login(t, adminUser, "admin")
	return e
}

type bresp struct {
	status int
	body   []byte
}

func (r bresp) into(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.body, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", r.body, err)
	}
}

func (e benv) do(t *testing.T, method, path, token string, body any) bresp {
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
	return bresp{status: res.StatusCode, body: raw}
}

func (e benv) login(t *testing.T, user, pass string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": pass})
	if r.status != http.StatusOK {
		t.Fatalf("login = %d: %s", r.status, r.body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	r.into(t, &out)
	return out.AccessToken
}

func (e benv) createProfile(t *testing.T, name string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles", e.token, map[string]any{
		"name": name, "price": 1000, "currency": "IQD", "duration_days": 30,
		"rate_down_kbps": 2048, "rate_up_kbps": 1024,
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

func (e benv) waitBulk(t *testing.T, jobID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r := e.do(t, "GET", "/api/v1/profiles/bulk/"+jobID, e.token, nil)
		if r.status != http.StatusOK {
			t.Fatalf("bulk status = %d: %s", r.status, r.body)
		}
		var out map[string]any
		r.into(t, &out)
		if out["status"] == "completed" {
			return out
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("bulk job did not complete")
	return nil
}

// TestBulkArchiveBySelection — an explicit id selection archives exactly those
// plans regardless of the filter.
func TestBulkArchiveBySelection(t *testing.T) {
	e := bsetup(t)
	a := e.createProfile(t, buniq("A_"))
	b := e.createProfile(t, buniq("B_"))

	r := e.do(t, "POST", "/api/v1/profiles/bulk", e.token, map[string]any{
		"profile_ids": []string{a}, "action": "archive",
	})
	if r.status != http.StatusAccepted {
		t.Fatalf("bulk archive = %d: %s", r.status, r.body)
	}
	var job struct {
		ID string `json:"id"`
	}
	r.into(t, &job)
	done := e.waitBulk(t, job.ID)
	if done["succeeded"].(float64) != 1 {
		t.Fatalf("succeeded = %v, want 1", done["succeeded"])
	}

	var aArch, bArch bool
	_ = e.db.QueryRow(t.Context(), `SELECT archived FROM profiles WHERE id=$1::uuid`, a).Scan(&aArch)
	_ = e.db.QueryRow(t.Context(), `SELECT archived FROM profiles WHERE id=$1::uuid`, b).Scan(&bArch)
	if !aArch || bArch {
		t.Fatalf("archived a=%v b=%v, want true/false", aArch, bArch)
	}
}

// TestBulkQuotaBehaviorThrottleRefused — throttle is refused up front (422) with
// no job started, because it needs a per-plan throttle rate.
func TestBulkQuotaBehaviorThrottleRefused(t *testing.T) {
	e := bsetup(t)
	a := e.createProfile(t, buniq("Q_"))
	r := e.do(t, "POST", "/api/v1/profiles/bulk", e.token, map[string]any{
		"profile_ids": []string{a}, "action": "set_quota_behavior",
		"params": map[string]any{"quota_behavior": "throttle"},
	})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("throttle bulk = %d, want 422: %s", r.status, r.body)
	}
}

// TestBulkDeleteInUseReportedPerRow — a bulk delete over a selection with one
// in-use plan fails that row (naming archive) and deletes the rest.
func TestBulkDeleteInUseReportedPerRow(t *testing.T) {
	e := bsetup(t)
	free := e.createProfile(t, buniq("Free_"))
	used := e.createProfile(t, buniq("Used_"))
	// Put a subscriber on `used` so the in-use guard trips.
	if _, err := e.db.Exec(t.Context(),
		`INSERT INTO subscribers (username, password_enc, profile_id, service_type)
		 VALUES ($1, '\x00', $2::uuid, 'pppoe')`, buniq("sub_"), used); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}

	r := e.do(t, "POST", "/api/v1/profiles/bulk", e.token, map[string]any{
		"profile_ids": []string{free, used}, "action": "delete",
	})
	if r.status != http.StatusAccepted {
		t.Fatalf("bulk delete = %d: %s", r.status, r.body)
	}
	var job struct {
		ID string `json:"id"`
	}
	r.into(t, &job)
	done := e.waitBulk(t, job.ID)
	if done["succeeded"].(float64) != 1 || done["failed"].(float64) != 1 {
		t.Fatalf("succeeded/failed = %v/%v, want 1/1", done["succeeded"], done["failed"])
	}

	var freeCount, usedCount int
	_ = e.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM profiles WHERE id=$1::uuid`, free).Scan(&freeCount)
	_ = e.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM profiles WHERE id=$1::uuid`, used).Scan(&usedCount)
	if freeCount != 0 || usedCount != 1 {
		t.Fatalf("free=%d used=%d, want 0/1 (in-use kept)", freeCount, usedCount)
	}
}
