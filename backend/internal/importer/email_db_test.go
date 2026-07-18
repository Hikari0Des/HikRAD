package importer_test

// DB-backed suite for the CSV importer's email column mapping (v2-6,
// FR-85.2, contract C5). Reuses the setup/env/uniq harness from db_test.go.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// TestImportMapsEmailColumn — gate item 8 (AC-85b, C5): the sas4 preset maps
// a header named "Email" case-insensitively; a dry run over rows with valid
// and malformed emails reports the malformed ones as per-row errors and
// writes zero rows; a subsequent import of the corrected file creates
// exactly the valid rows with email populated.
func TestImportMapsEmailColumn(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("Package_"), 10_000, 30)
	pkgName := e.profileName(t, profID)

	goodUser := uniq("sas4_email_good_")
	badUser := uniq("sas4_email_bad_")
	header := "UserName,Password,FullName,Mobile,Package,ExpireDate,Email\n"
	rows := fmt.Sprintf("%s,pass123,Good Email,,%s,2027-01-01,good@example.com\n", goodUser, pkgName) +
		fmt.Sprintf("%s,pass123,Bad Email,,%s,2027-01-01,not-an-email\n", badUser, pkgName)

	up := e.upload(t, "sas4_email.csv", []byte(header+rows), "sas4")
	batchID, _ := up["batch_id"].(string)
	if batchID == "" {
		t.Fatal("no batch_id in upload response")
	}
	colMap, _ := up["column_map"].(map[string]any)
	if colMap["email"] != "Email" {
		t.Fatalf("sas4 preset did not map the Email header: column_map = %v", colMap)
	}

	dr := e.do(t, "POST", "/api/v1/import/"+batchID+"/dry-run", e.token, map[string]any{})
	if dr.status != http.StatusOK {
		t.Fatalf("dry-run = %d: %s", dr.status, dr.body)
	}
	var drOut struct {
		Rows []struct {
			Fields map[string]string `json:"fields"`
			Errors []string          `json:"errors"`
		} `json:"rows"`
		WillCreate int `json:"will_create"`
	}
	dr.into(t, &drOut)

	var badRowErrored, goodRowClean bool
	for _, rr := range drOut.Rows {
		switch rr.Fields["username"] {
		case badUser:
			for _, msg := range rr.Errors {
				if msg == "email is not a valid email address" {
					badRowErrored = true
				}
			}
		case goodUser:
			goodRowClean = len(rr.Errors) == 0
		}
	}
	if !badRowErrored {
		t.Fatalf("malformed email did not produce a dry-run error: %+v", drOut.Rows)
	}
	if !goodRowClean {
		t.Fatalf("valid-email row unexpectedly errored: %+v", drOut.Rows)
	}
	if drOut.WillCreate != 1 {
		t.Fatalf("will_create = %d, want 1 (only the good row)", drOut.WillCreate)
	}

	// Zero writes from dry-run.
	var count int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM subscribers WHERE username IN ($1::citext, $2::citext)`,
		goodUser, badUser).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("dry-run wrote %d subscriber rows, want 0", count)
	}

	ex := e.do(t, "POST", "/api/v1/import/"+batchID+"/execute", e.token, map[string]any{})
	if ex.status != http.StatusAccepted {
		t.Fatalf("execute = %d: %s", ex.status, ex.body)
	}
	final := e.waitExecuted(t, batchID)
	summary, _ := final["summary"].(map[string]any)
	if summary["created"] != float64(1) {
		t.Fatalf("execute summary.created = %v, want 1", summary["created"])
	}

	var email string
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE(email,'') FROM subscribers WHERE username = $1::citext`, goodUser).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "good@example.com" {
		t.Fatalf("imported email = %q, want good@example.com", email)
	}
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM subscribers WHERE username = $1::citext`, badUser).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("the malformed-email row was created (count=%d), want 0", count)
	}
}
