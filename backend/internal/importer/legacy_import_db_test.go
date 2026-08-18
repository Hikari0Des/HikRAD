package importer_test

// End-to-end DB coverage for FR-138: a Radius Manager-shaped MySQL dump goes
// through the SAME upload → map → dry-run → execute pipeline the SAS4 CSV
// suite already proves, converted to CSV at the upload edge (api.go) rather
// than through a separate import path.

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
)

func (e env) subscriberCount(t *testing.T, username string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM subscribers WHERE username = $1`, username).Scan(&n); err != nil {
		t.Fatalf("count subscribers: %v", err)
	}
	return n
}

func TestImportLegacyDump_RadiusManagerSubscribers(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("Package_"), 10_000, 30)
	pkgName := e.profileName(t, profID)

	user1 := uniq("rm_user_")
	user2 := uniq("rm_user_")
	dump := fmt.Sprintf(
		"-- Radius Manager export\n"+
			"INSERT INTO `users` (`username`, `password`, `fullname`, `phone`, `planname`, `expiration`) VALUES\n"+
			"('%s', 'pw123', 'Ali Hassan', '07701234567', '%s', '2027-01-01'),\n"+
			"('%s', 'pw456', 'Sara Kamal', '07709876543', '%s', '2027-06-15');\n",
		user1, pkgName, user2, pkgName)

	r := e.do(t, "POST", "/api/v1/import/subscribers", e.token, map[string]any{
		"filename": "rm_export.sql", "content_base64": base64.StdEncoding.EncodeToString([]byte(dump)),
		"source_system": "radius_manager",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("upload = %d: %s", r.status, r.body)
	}
	var up map[string]any
	r.into(t, &up)
	if up["status"] != "mapped" {
		t.Fatalf("upload status = %v, want mapped (the radius_manager preset should auto-apply)", up["status"])
	}
	header, _ := up["header"].([]any)
	if len(header) != 6 {
		t.Fatalf("header = %v, want 6 columns extracted from the INSERT's own column list", header)
	}
	batchID, _ := up["batch_id"].(string)
	if batchID == "" {
		t.Fatal("no batch_id in upload response")
	}

	dr := e.do(t, "POST", "/api/v1/import/"+batchID+"/dry-run", e.token, map[string]any{})
	if dr.status != http.StatusOK {
		t.Fatalf("dry-run = %d: %s", dr.status, dr.body)
	}
	var drOut struct {
		WillCreate int `json:"will_create"`
		WillSkip   int `json:"will_skip"`
	}
	dr.into(t, &drOut)
	if drOut.WillCreate != 2 || drOut.WillSkip != 0 {
		t.Fatalf("dry-run will_create=%d will_skip=%d, want 2/0", drOut.WillCreate, drOut.WillSkip)
	}

	// Zero subscribers written by dry-run.
	if n := e.subscriberCount(t, user1); n != 0 {
		t.Fatalf("dry-run wrote a subscriber for %s", user1)
	}

	ex := e.do(t, "POST", "/api/v1/import/"+batchID+"/execute", e.token, map[string]any{})
	if ex.status != http.StatusAccepted {
		t.Fatalf("execute = %d: %s", ex.status, ex.body)
	}
	final := e.waitExecuted(t, batchID)
	summary, _ := final["summary"].(map[string]any)
	if summary["created"] != float64(2) {
		t.Fatalf("summary = %+v, want created=2", summary)
	}
	if e.subscriberCount(t, user1) != 1 || e.subscriberCount(t, user2) != 1 {
		t.Fatalf("expected both dump-sourced subscribers to exist: %s=%d %s=%d",
			user1, e.subscriberCount(t, user1), user2, e.subscriberCount(t, user2))
	}

	// The batch records which legacy system it came from.
	status := e.do(t, "GET", "/api/v1/import/"+batchID, e.token, nil)
	var statusOut map[string]any
	status.into(t, &statusOut)
	if statusOut["source_system"] != "radius_manager" {
		t.Fatalf("source_system = %v, want radius_manager", statusOut["source_system"])
	}
}
