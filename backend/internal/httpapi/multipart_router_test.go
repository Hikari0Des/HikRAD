package httpapi

// FR-127 gate item 13: TestContentTypeEnforced (devauth_test.go) only ever
// proved the REJECTION half of enforceJSON's contract (a bad Content-Type
// gets a 415) — nothing asserted that a legitimate multipart/form-data
// request actually gets past the middleware through the REAL router, which
// is exactly the class of bug the v2-11 incident was (a global JSON-only
// middleware silently 415'd every upload; see enforceJSON's own doc
// comment and docs/ops/known-issues.md). This closes that gap without a
// database: it targets a real registered route (auth/login) through a real
// httpapi.NewRouter instance and asserts the response is NOT 415 — i.e.
// enforceJSON let the request continue to the handler, whatever that
// handler then does with a non-JSON body. The DB-backed complement —
// internal/platform/setupapi's TestBrandingChangesAudited — proves a real
// multipart-accepting business handler (the logo upload) succeeds
// end-to-end through this exact router construction when a database is
// available; this test proves the middleware layer alone, deterministically,
// in every CI run regardless of DB availability.

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func multipartBody(t *testing.T, fieldName, value string) (body *bytes.Buffer, contentType string) {
	t.Helper()
	body = &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField(fieldName, value); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body, w.FormDataContentType()
}

func TestEnforceJSON_AllowsMultipartThroughRealRouter(t *testing.T) {
	h := devLoginRouter(t)
	body, ct := multipartBody(t, "note", "not a real login body")

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	r.Header.Set("Content-Type", ct)
	h.ServeHTTP(rec, r)

	if rec.Code == http.StatusUnsupportedMediaType {
		t.Fatalf("multipart/form-data request was rejected 415 by the real router — enforceJSON regressed to the v2-11 bug (see docs/ops/known-issues.md)")
	}
}

// TestEnforceJSON_StillRejectsGenuinelyInvalidTypes is the control case
// proving the previous test isn't vacuous (e.g. enforceJSON disabled
// entirely would also make it pass): a real invalid Content-Type through
// the same real router still gets 415.
func TestEnforceJSON_StillRejectsGenuinelyInvalidTypes(t *testing.T) {
	h := devLoginRouter(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString("username=a&password=b"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415 for a genuinely invalid Content-Type", rec.Code)
	}
}
