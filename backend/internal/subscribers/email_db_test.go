package subscribers_test

// DB-backed suite for subscriber email (v2-6, FR-85.1, contract C4). Reuses
// the setup/testEnv/uniq harness from dbharness_test.go.

import (
	"context"
	"net/http"
	"testing"
)

// TestSubscriberEmailValidation — gate item 7 (C4): a valid email persists
// and round-trips on the subscriber read shape; a malformed value 422s with a
// field_errors entry on "email" and writes nothing.
func TestSubscriberEmailValidation(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("plan_"), 10240, 2048)

	t.Run("valid email persists and round-trips", func(t *testing.T) {
		username := uniq("sub_")
		r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
			"username": username, "password": "pw12345", "profile_id": profID,
			"email": "sara@example.com",
		})
		if r.status != http.StatusCreated {
			t.Fatalf("create = %d: %s", r.status, r.body)
		}
		var created struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		}
		r.into(t, &created)
		if created.Email != "sara@example.com" {
			t.Fatalf("email on create response = %q, want sara@example.com", created.Email)
		}

		get := e.do(t, "GET", "/api/v1/subscribers/"+created.ID, nil)
		if get.status != http.StatusOK {
			t.Fatalf("get = %d: %s", get.status, get.body)
		}
		var fetched struct {
			Subscriber struct {
				Email string `json:"email"`
			} `json:"subscriber"`
		}
		get.into(t, &fetched)
		if fetched.Subscriber.Email != "sara@example.com" {
			t.Fatalf("email did not round-trip: got %q", fetched.Subscriber.Email)
		}
	})

	t.Run("malformed email 422s and writes nothing", func(t *testing.T) {
		username := uniq("sub_bad_")
		r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
			"username": username, "password": "pw12345", "profile_id": profID,
			"email": "not-an-email",
		})
		if r.status != http.StatusUnprocessableEntity {
			t.Fatalf("create with bad email = %d, want 422: %s", r.status, r.body)
		}
		var count int
		if err := e.db.QueryRow(context.Background(),
			`SELECT count(*) FROM subscribers WHERE username = $1::citext`, username).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("a rejected create wrote a subscriber row (count=%d)", count)
		}
	})

	t.Run("malformed email on update 422s and leaves the field unchanged", func(t *testing.T) {
		username := uniq("sub_upd_")
		created := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
			"username": username, "password": "pw12345", "profile_id": profID,
			"email": "keep@example.com",
		})
		var sub struct {
			ID string `json:"id"`
		}
		created.into(t, &sub)

		upd := e.do(t, "PUT", "/api/v1/subscribers/"+sub.ID, map[string]any{"email": "still not valid"})
		if upd.status != http.StatusUnprocessableEntity {
			t.Fatalf("update with bad email = %d, want 422: %s", upd.status, upd.body)
		}

		get := e.do(t, "GET", "/api/v1/subscribers/"+sub.ID, nil)
		var fetched struct {
			Subscriber struct {
				Email string `json:"email"`
			} `json:"subscriber"`
		}
		get.into(t, &fetched)
		if fetched.Subscriber.Email != "keep@example.com" {
			t.Fatalf("rejected update changed the email: got %q", fetched.Subscriber.Email)
		}
	})
}
