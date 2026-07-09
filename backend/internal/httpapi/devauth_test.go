package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func testDeps() Deps {
	return Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// stubManagers replaces the DB lookup with a fixed manager set for the test.
func stubManagers(t *testing.T, byUsername map[string]managerRecord) {
	t.Helper()
	orig := lookupManager
	lookupManager = func(_ context.Context, _ *pgxpool.Pool, username string) (*managerRecord, error) {
		if m, ok := byUsername[username]; ok {
			return &m, nil
		}
		return nil, nil
	}
	t.Cleanup(func() { lookupManager = orig })
}

func devLoginRouter(t *testing.T) http.Handler {
	t.Helper()
	EnableDevLogin(testSecret)
	t.Cleanup(func() { devLoginSecret = nil })
	return NewRouter(testDeps(), true)
}

func postLogin(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, r)
	return rec
}

func TestDevLoginAccept(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	stubManagers(t, map[string]managerRecord{
		"admin": {ID: "mgr-1", Username: "admin", PasswordHash: string(hash), Role: "admin"},
	})
	h := devLoginRouter(t)

	rec := postLogin(h, `{"username":"admin","password":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Manager      struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"manager"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("tokens missing")
	}
	if resp.Manager.ID != "mgr-1" || resp.Manager.Username != "admin" || resp.Manager.Role != "admin" {
		t.Fatalf("manager = %+v", resp.Manager)
	}

	// The issued access token must satisfy the stub authenticator.
	auth := JWTAuthenticator{Secret: testSecret}
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Authorization", "Bearer "+resp.AccessToken)
	id, err := auth.Authenticate(r)
	if err != nil || id.ManagerID != "mgr-1" || id.Role != "admin" {
		t.Fatalf("issued token rejected: id=%+v err=%v", id, err)
	}
}

func TestDevLoginReject(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	stubManagers(t, map[string]managerRecord{
		"admin": {ID: "mgr-1", Username: "admin", PasswordHash: string(hash), Role: "admin"},
	})
	h := devLoginRouter(t)

	for name, body := range map[string]string{
		"wrong password": `{"username":"admin","password":"nope"}`,
		"unknown user":   `{"username":"ghost","password":"admin"}`,
	} {
		rec := postLogin(h, body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"invalid_credentials"`) {
			t.Fatalf("%s: body = %s", name, rec.Body.String())
		}
	}

	rec := postLogin(h, `{"username":"admin"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing password: status = %d, want 422", rec.Code)
	}
}

func TestDevLoginAbsentOutsideDevMode(t *testing.T) {
	// EnableDevLogin never called → module registers nothing.
	h := NewRouter(testDeps(), false)
	rec := postLogin(h, `{"username":"admin","password":"admin"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (login stub must not exist outside dev)", rec.Code)
	}
}

func TestRouterPanicBecomes500EnvelopeWithRequestID(t *testing.T) {
	Add(panicModule{})
	t.Cleanup(func() { registry = registry[:len(registry)-1] })

	h := NewRouter(testDeps(), true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/test/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("panic response is not the C2 envelope: %s", rec.Body.String())
	}
	reqID := rec.Header().Get("X-Request-Id")
	if reqID == "" {
		t.Fatal("X-Request-Id header missing")
	}
	if body.Error.Code != "internal" || !strings.Contains(body.Error.Message, reqID) {
		t.Fatalf("panic envelope must carry the request ID: %+v (header %q)", body.Error, reqID)
	}
}

type panicModule struct{}

func (panicModule) Name() string { return "test-panic" }
func (panicModule) Register(r chi.Router, _ Deps) {
	r.Get("/test/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
}

func TestContentTypeEnforced(t *testing.T) {
	h := devLoginRouter(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"a","password":"b"}`))
	r.Header.Set("Content-Type", "text/plain")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}
