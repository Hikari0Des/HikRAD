package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

type validateFixture struct {
	Username string `json:"username" validate:"required"`
	Service  string `json:"service" validate:"required,oneof=pppoe hotspot"`
	Note     string `json:"note"`
}

func TestValidateReportsJSONFieldNames(t *testing.T) {
	fields := Validate(validateFixture{Service: "dsl"})
	if len(fields) != 2 {
		t.Fatalf("want 2 field errors, got %+v", fields)
	}
	byField := map[string]string{}
	for _, f := range fields {
		byField[f.Field] = f.Message
	}
	if byField["username"] != "this field is required" {
		t.Fatalf("username: %q", byField["username"])
	}
	if !strings.Contains(byField["service"], "pppoe hotspot") {
		t.Fatalf("service: %q", byField["service"])
	}
}

func TestValidateOK(t *testing.T) {
	if fields := Validate(validateFixture{Username: "u", Service: "pppoe"}); fields != nil {
		t.Fatalf("valid struct produced field errors: %+v", fields)
	}
}

func TestBindMalformedJSONIs400(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/x", strings.NewReader("{nope"))
	var dst validateFixture
	if Bind(rec, r, &dst) {
		t.Fatal("Bind should fail on malformed JSON")
	}
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBindValidationFailureIs422WithFieldErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"service":"pppoe"}`))
	var dst validateFixture
	if Bind(rec, r, &dst) {
		t.Fatal("Bind should fail validation")
	}
	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"username"`) {
		t.Fatalf("body should name the username field: %s", rec.Body.String())
	}
}
