package main

// Integration test per the Phase-1 Agent-3 Definition of Done: boot the
// server wiring against a real Postgres (migrations → seed → registered
// modules) and hit the seeded endpoints. Gated on HIKRAD_TEST_DB_URL, which
// CI provides (see .github/workflows/ci.yml); locally:
//
//	HIKRAD_TEST_DB_URL=postgres://user:pass@localhost:5432/hikrad_test?sslmode=disable go test ./cmd/hikrad-api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/seed"
)

func TestIntegration(t *testing.T) {
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	encryptionKey := make([]byte, 32)
	if _, err := rand.Read(encryptionKey); err != nil {
		t.Fatal(err)
	}
	jwtSecret := []byte("integration-test-secret")

	// NewRouter (below) registers every module including radius, whose
	// Register re-reads platform.LoadConfig() for the encryption key (Deps
	// carries no such field — see internal/radius/authorize.go). main()
	// always has the full HIKRAD_* environment validated before it gets this
	// far, so this test — which builds the router directly, bypassing
	// main()/buildDeps — sets the same environment to match.
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", "redis://unused:6379/0")
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(encryptionKey))
	t.Setenv("HIKRAD_JWT_SECRET", string(jwtSecret))

	// Boot sequence exactly as run(): migrate, connect, seed, wire seams.
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	if err := seed.Run(ctx, db, encryptionKey); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := seed.Run(ctx, db, encryptionKey); err != nil {
		t.Fatalf("seed must be idempotent, rerun failed: %v", err)
	}

	httpapi.SetAuthenticator(httpapi.JWTAuthenticator{Secret: jwtSecret})
	httpapi.EnableDevLogin(jwtSecret)
	deps := httpapi.Deps{DB: db, Settings: platform.NewSettings(db), Log: log}
	srv := httptest.NewServer(httpapi.NewRouter(deps, true))
	defer srv.Close()

	access := loginAdmin(t, srv.URL)

	t.Run("subscribers list requires auth", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/subscribers")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("seeded subscriber listed with envelope and pagination", func(t *testing.T) {
		var list struct {
			Items []struct {
				Username string  `json:"username"`
				Status   string  `json:"status"`
				Password *string `json:"password_enc"` // must never appear
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		body := getJSON(t, srv.URL+"/api/v1/subscribers", access, &list)
		found := false
		for _, it := range list.Items {
			if it.Username == "testuser" {
				found = true
				if it.Status != "active" {
					t.Fatalf("testuser status = %q", it.Status)
				}
			}
		}
		if !found {
			t.Fatalf("testuser not in %s", body)
		}
		if bytes.Contains([]byte(body), []byte("password")) {
			t.Fatalf("password material leaked in list response: %s", body)
		}
	})

	t.Run("seeded profile listed", func(t *testing.T) {
		var list struct {
			Items []struct {
				Name         string `json:"name"`
				PriceIQD     int64  `json:"price_iqd"`
				DurationDays int    `json:"duration_days"`
			} `json:"items"`
		}
		body := getJSON(t, srv.URL+"/api/v1/profiles", access, &list)
		found := false
		for _, p := range list.Items {
			if p.Name == "Basic 10M" && p.PriceIQD == 25000 && p.DurationDays == 30 {
				found = true
			}
		}
		if !found {
			t.Fatalf("Basic 10M profile not in %s", body)
		}
	})

	t.Run("keyset pagination is stable and complete", func(t *testing.T) {
		// Add extra rows, then walk limit=2 pages and check every subscriber
		// appears exactly once.
		for i := 0; i < 5; i++ {
			_, err := db.Exec(ctx,
				`INSERT INTO subscribers (username, status) VALUES ($1, 'active')
				 ON CONFLICT (username) DO NOTHING`,
				fmt.Sprintf("pagetest-%d", i))
			if err != nil {
				t.Fatal(err)
			}
		}
		seen := map[string]int{}
		next := ""
		for pages := 0; ; pages++ {
			if pages > 20 {
				t.Fatal("pagination did not terminate")
			}
			url := srv.URL + "/api/v1/subscribers?limit=2"
			if next != "" {
				url += "&cursor=" + next
			}
			var list struct {
				Items []struct {
					Username string `json:"username"`
				} `json:"items"`
				NextCursor *string `json:"next_cursor"`
			}
			getJSON(t, url, access, &list)
			if len(list.Items) > 2 {
				t.Fatalf("page larger than limit: %d", len(list.Items))
			}
			for _, it := range list.Items {
				seen[it.Username]++
			}
			if list.NextCursor == nil {
				break
			}
			next = *list.NextCursor
		}
		if seen["testuser"] != 1 {
			t.Fatalf("testuser seen %d times", seen["testuser"])
		}
		for i := 0; i < 5; i++ {
			u := fmt.Sprintf("pagetest-%d", i)
			if seen[u] != 1 {
				t.Fatalf("%s seen %d times across pages", u, seen[u])
			}
		}
	})

	t.Run("seeded password decrypts (AES-GCM through the DB)", func(t *testing.T) {
		var enc []byte
		if err := db.QueryRow(ctx, `SELECT password_enc FROM subscribers WHERE username = 'testuser'`).Scan(&enc); err != nil {
			t.Fatal(err)
		}
		got, err := seed.DecryptPassword(enc, encryptionKey)
		if err != nil || got != "testpass" {
			t.Fatalf("decrypt = %q, %v", got, err)
		}
	})

	t.Run("radius authorize stub mounted on /internal", func(t *testing.T) {
		resp := postJSON(t, srv.URL+"/internal/radius/authorize",
			`{"username":"testuser","password":"testpass","nas_ip":"10.0.0.1","service":"pppoe"}`)
		if resp.Action != "accept" || resp.Reason != "ok" {
			t.Fatalf("authorize = %+v", resp)
		}
		if len(resp.Attributes) != 1 || resp.Attributes[0].Intent != "rate_limit" || resp.Attributes[0].Value != "10M/10M" {
			t.Fatalf("attributes = %+v", resp.Attributes)
		}

		resp = postJSON(t, srv.URL+"/internal/radius/authorize",
			`{"username":"testuser","password":"wrong","nas_ip":"10.0.0.1","service":"pppoe"}`)
		if resp.Action != "reject" || resp.Reason != "bad_password" {
			t.Fatalf("authorize wrong password = %+v", resp)
		}
	})

	t.Run("login rejects bad credentials", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json",
			bytes.NewReader([]byte(`{"username":"admin","password":"wrong"}`)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})
}

func loginAdmin(t *testing.T, baseURL string) string {
	t.Helper()
	resp, err := http.Post(baseURL+"/api/v1/auth/login", "application/json",
		bytes.NewReader([]byte(`{"username":"admin","password":"admin"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Manager      struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"manager"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("login response: %v (%s)", err, body)
	}
	if out.AccessToken == "" || out.RefreshToken == "" || out.Manager.Username != "admin" {
		t.Fatalf("login shape wrong: %s", body)
	}
	return out.AccessToken
}

func getJSON(t *testing.T, url, access string, dst any) string {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("GET %s: %v (%s)", url, err, body)
	}
	return string(body)
}

type authorizeResult struct {
	Action     string `json:"action"`
	Reason     string `json:"reason"`
	Attributes []struct {
		Intent string `json:"intent"`
		Value  string `json:"value"`
	} `json:"attributes"`
}

func postJSON(t *testing.T, url, body string) authorizeResult {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d: %s", url, resp.StatusCode, raw)
	}
	var out authorizeResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("POST %s: %v (%s)", url, err, raw)
	}
	return out
}
