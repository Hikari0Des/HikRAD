package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestErrorEnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, 422, "validation_failed", "request validation failed",
		FieldError{Field: "username", Message: "this field is required"})

	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}

	// Decode into a raw map to assert the exact C2 wire shape.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	rawErr, ok := body["error"]
	if !ok || len(body) != 1 {
		t.Fatalf("body must be exactly {\"error\":{...}}, got %s", rec.Body.String())
	}
	var e struct {
		Code        string       `json:"code"`
		Message     string       `json:"message"`
		FieldErrors []FieldError `json:"field_errors"`
	}
	if err := json.Unmarshal(rawErr, &e); err != nil {
		t.Fatalf("error object malformed: %v", err)
	}
	if e.Code != "validation_failed" || e.Message != "request validation failed" {
		t.Fatalf("unexpected code/message: %+v", e)
	}
	if len(e.FieldErrors) != 1 || e.FieldErrors[0].Field != "username" {
		t.Fatalf("unexpected field_errors: %+v", e.FieldErrors)
	}
}

func TestErrorEnvelopeFieldErrorsAlwaysPresent(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, 404, "not_found", "resource not found")

	var body struct {
		Error struct {
			FieldErrors json.RawMessage `json:"field_errors"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if string(body.Error.FieldErrors) != "[]" {
		t.Fatalf("field_errors = %s, want [] (always present, empty array)", body.Error.FieldErrors)
	}
}
