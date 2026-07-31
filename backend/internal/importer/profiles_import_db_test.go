package importer_test

// DB-backed profile-import suite (FR-111.3, v3-6). Gated on HIKRAD_TEST_DB_URL,
// reuses the env/setup/do helpers in db_test.go. Proves the entity-parameterized
// wizard loads a PLAN catalogue through the real profiles API, with the
// structural difference from the subscriber import: a duplicate plan name is a
// dry-run skip (profiles.name is not unique, so there is no create-time 409 to
// lean on), and re-execute creates nothing further.

import (
	"encoding/base64"
	"net/http"
	"testing"
)

func (e env) uploadEntity(t *testing.T, entity, filename string, raw []byte, preset string) map[string]any {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/import/"+entity, e.token, map[string]any{
		"filename": filename, "content_base64": base64.StdEncoding.EncodeToString(raw), "preset": preset,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("upload %s = %d: %s", entity, r.status, r.body)
	}
	var out map[string]any
	r.into(t, &out)
	return out
}

func TestImportProfilesFlow(t *testing.T) {
	e := setup(t)

	// A plan name that already exists — its import row must be skipped.
	existing := uniq("Plan_Existing_")
	e.createProfile(t, existing, 5_000, 30)

	newPlan := uniq("Plan_New_")
	header := "Name,Price,Currency,Days,DownloadRate,UploadRate,Quota\n"
	body := header +
		newPlan + ",10000,IQD,30,10M,2M,\n" +
		existing + ",7000,IQD,30,5M,1M,\n" // duplicate name → skip

	up := e.uploadEntity(t, "profiles", "plans.csv", []byte(body), "sas4")
	if up["entity"] != "profiles" {
		t.Fatalf("entity = %v, want profiles", up["entity"])
	}
	batchID, _ := up["batch_id"].(string)

	// Map is preset-seeded (sas4 profiles preset); dry-run.
	dr := e.do(t, "POST", "/api/v1/import/"+batchID+"/dry-run", e.token, nil)
	if dr.status != http.StatusOK {
		t.Fatalf("dry-run = %d: %s", dr.status, dr.body)
	}
	var report struct {
		WillCreate int `json:"will_create"`
		WillSkip   int `json:"will_skip"`
	}
	dr.into(t, &report)
	if report.WillCreate != 1 || report.WillSkip != 1 {
		t.Fatalf("dry-run create/skip = %d/%d, want 1/1", report.WillCreate, report.WillSkip)
	}

	// Execute → the new plan is created, the duplicate is skipped.
	ex := e.do(t, "POST", "/api/v1/import/"+batchID+"/execute", e.token, nil)
	if ex.status != http.StatusAccepted {
		t.Fatalf("execute = %d: %s", ex.status, ex.body)
	}
	e.waitExecuted(t, batchID)

	var count int
	if err := e.db.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM profiles WHERE name = $1`, newPlan).Scan(&count); err != nil {
		t.Fatalf("count new plan: %v", err)
	}
	if count != 1 {
		t.Fatalf("new plan count = %d, want 1", count)
	}

	// Re-execute is idempotent: nothing new created.
	ex2 := e.do(t, "POST", "/api/v1/import/"+batchID+"/execute", e.token, nil)
	if ex2.status != http.StatusAccepted {
		t.Fatalf("re-execute = %d: %s", ex2.status, ex2.body)
	}
	e.waitExecuted(t, batchID)
	if err := e.db.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM profiles WHERE name = $1`, newPlan).Scan(&count); err != nil {
		t.Fatalf("recount new plan: %v", err)
	}
	if count != 1 {
		t.Fatalf("after re-execute new plan count = %d, want 1 (idempotent)", count)
	}
}
