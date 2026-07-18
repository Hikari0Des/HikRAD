package setupapi

// DB-backed tests for the logo upload/delete endpoints and the
// generic-PUT rejection (v2 phase 11, FR-91, contract C3). Reuses
// setupWizardEnv/wizCall (wizard_db_test.go) for a throwaway DB + admin
// token, since these tests care about a clean audit_log count.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"testing"
)

// adminToken creates the first admin and logs in, returning an access token.
func adminToken(t *testing.T, e wizardEnv) string {
	t.Helper()
	r := wizCall(t, e, "POST", "/api/v1/setup/admin", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	if r.status != http.StatusCreated {
		t.Fatalf("create admin: %d: %s", r.status, r.body)
	}
	r = wizCall(t, e, "POST", "/api/v1/auth/login", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	if r.status != http.StatusOK {
		t.Fatalf("login: %d: %s", r.status, r.body)
	}
	var login struct {
		AccessToken string `json:"access_token"`
	}
	r.json(t, &login)
	return login.AccessToken
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multipartLogo(t *testing.T, data []byte) (body *bytes.Buffer, contentType string) {
	t.Helper()
	body = &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("logo", "logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body, w.FormDataContentType()
}

func TestBrandingGroupPutRejectsLogoURL(t *testing.T) {
	e := setupWizardEnv(t)
	token := adminToken(t, e)

	req, _ := http.NewRequest(http.MethodPut, e.srv.URL+"/api/v1/settings/branding",
		bytes.NewReader(mustJSON(t, map[string]any{"logo_url": "/api/v1/branding/logo?v=abc123"})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("PUT branding with logo_url in body = %d, want 422", res.StatusCode)
	}

	// name/colors still work through the same endpoint.
	r := wizCall(t, e, "PUT", "/api/v1/settings/branding", token,
		mustJSON(t, map[string]any{"name": "Tigris Net"}))
	if r.status != http.StatusOK {
		t.Fatalf("PUT branding without logo_url = %d: %s", r.status, r.body)
	}
}

func TestBrandingChangesAudited(t *testing.T) {
	// platform.BrandingDir() resolves HIKRAD_BRANDING_DIR exactly once per
	// process (sync.Once); set it before the first StoreLogo call in this
	// test binary so the upload never lands under the repo tree's default
	// data/branding path.
	t.Setenv("HIKRAD_BRANDING_DIR", t.TempDir())

	e := setupWizardEnv(t)
	token := adminToken(t, e)
	ctx := context.Background()

	var before int
	_ = e.db.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND entity_id = 'branding'`).Scan(&before)

	// Name change via the generic group PUT.
	r := wizCall(t, e, "PUT", "/api/v1/settings/branding", token,
		mustJSON(t, map[string]any{"name": "Tigris Net"}))
	if r.status != http.StatusOK {
		t.Fatalf("PUT branding: %d: %s", r.status, r.body)
	}

	// Logo upload.
	body, ct := multipartLogo(t, tinyPNG(t))
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1/settings/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("logo upload = %d", res.StatusCode)
	}
	t.Cleanup(func() {
		delReq, _ := http.NewRequest(http.MethodDelete, e.srv.URL+"/api/v1/settings/branding/logo", nil)
		delReq.Header.Set("Authorization", "Bearer "+token)
		if resp, err := http.DefaultClient.Do(delReq); err == nil {
			resp.Body.Close()
		}
	})

	var after int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND entity_id = 'branding'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after < before+2 {
		t.Errorf("audit_log settings.update(branding) rows = %d, want at least %d (name change + logo upload)", after, before+2)
	}
}
