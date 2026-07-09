package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var testSecret = []byte("test-jwt-secret")

func protectedHandler(t *testing.T, sawIdentity *Identity) http.Handler {
	t.Helper()
	return RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFrom(r.Context())
		if !ok {
			t.Error("identity missing from context inside protected handler")
		}
		*sawIdentity = id
		w.WriteHeader(http.StatusOK)
	}))
}

func TestRequireAuthAcceptsValidAccessToken(t *testing.T) {
	SetAuthenticator(JWTAuthenticator{Secret: testSecret})
	t.Cleanup(func() { SetAuthenticator(nil) })

	access, _, err := IssueTokens(testSecret, "mgr-1", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var id Identity
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Authorization", "Bearer "+access)
	protectedHandler(t, &id).ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if id.ManagerID != "mgr-1" || id.Role != "admin" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestRequireAuthRejects(t *testing.T) {
	SetAuthenticator(JWTAuthenticator{Secret: testSecret})
	t.Cleanup(func() { SetAuthenticator(nil) })

	access, refresh, err := IssueTokens(testSecret, "mgr-1", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	otherAccess, _, err := IssueTokens([]byte("wrong-secret"), "mgr-1", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	expiredAccess, _, err := IssueTokens(testSecret, "mgr-1", "admin", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"no header":               "",
		"not bearer":              "Basic abc",
		"garbage token":           "Bearer garbage",
		"wrong signature":         "Bearer " + otherAccess,
		"refresh used as access":  "Bearer " + refresh,
		"expired token":           "Bearer " + expiredAccess,
		"valid token, lowercased": "bearer " + access,
	}
	for name, header := range cases {
		var id Identity
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/x", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("%s: protected handler must not run", name)
		})).ServeHTTP(rec, r)
		_ = id
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", name, rec.Code)
		}
	}
}

func TestRequireAuthWithoutAuthenticatorIs401(t *testing.T) {
	SetAuthenticator(nil)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler must not run")
	})).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
