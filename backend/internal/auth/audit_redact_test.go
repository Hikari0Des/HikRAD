package auth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A struct carrying a time.Time (custom JSON marshaler) must serialize the
// timestamp as an RFC3339 string in the audit image, not field-walk it to {}.
func TestRedactPreservesMarshalerTypes(t *testing.T) {
	type withTime struct {
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}
	ts := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	raw, err := redactJSON(withTime{Name: "x", CreatedAt: ts})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "2026-07-09T12:00:00Z") {
		t.Fatalf("time.Time not preserved: %s", raw)
	}
}

type secretBearing struct {
	Username    string `json:"username"`
	PasswordEnc []byte `json:"password_enc" audit:"secret"`
	Secret      string `json:"secret,omitempty" audit:"secret"`
	Nested      *inner `json:"nested,omitempty"`
	Ignored     string `json:"-"`
}

type inner struct {
	SNMPCommunity string `json:"snmp_community" audit:"secret"`
	Location      string `json:"location"`
}

func TestRedactSecretFields(t *testing.T) {
	in := secretBearing{
		Username:    "alice",
		PasswordEnc: []byte{1, 2, 3},
		Secret:      "hunter2",
		Nested:      &inner{SNMPCommunity: "public", Location: "Baghdad"},
		Ignored:     "should not appear",
	}
	raw, err := redactJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "hunter2") || strings.Contains(s, "public") {
		t.Fatalf("secret leaked: %s", s)
	}
	if strings.Contains(s, "AQID") { // base64 of {1,2,3}
		t.Fatalf("password_enc bytes leaked: %s", s)
	}
	if strings.Contains(s, "should not appear") {
		t.Fatalf("json:\"-\" field leaked: %s", s)
	}
	// Non-secret fields survive.
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["username"] != "alice" {
		t.Fatalf("username missing/wrong: %v", out)
	}
	if out["password_enc"] != redacted || out["secret"] != redacted {
		t.Fatalf("secret fields not redacted: %v", out)
	}
	nested, ok := out["nested"].(map[string]any)
	if !ok || nested["snmp_community"] != redacted || nested["location"] != "Baghdad" {
		t.Fatalf("nested redaction wrong: %v", out["nested"])
	}
}

func TestRedactNilIsNull(t *testing.T) {
	raw, err := redactJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Fatalf("nil input must marshal to nil (SQL NULL), got %q", raw)
	}
}

func TestRedactOmitemptySecretHidden(t *testing.T) {
	// An empty omitempty secret should not surface a key at all.
	raw, _ := redactJSON(secretBearing{Username: "bob"})
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if _, present := out["secret"]; present {
		t.Fatalf("empty omitempty secret should be omitted: %v", out)
	}
	if out["username"] != "bob" {
		t.Fatalf("username wrong: %v", out)
	}
}
