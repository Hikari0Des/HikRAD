package httpapi

import (
	"encoding/json"
	"net/http"
)

// FieldError is one entry of the C2 envelope's field_errors array.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type errorBody struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"field_errors"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// Error writes the C2 error envelope:
//
//	{"error":{"code":"...","message":"...","field_errors":[{"field":"...","message":"..."}]}}
//
// field_errors is always present (empty array when there are none).
func Error(w http.ResponseWriter, status int, code, message string, fields ...FieldError) {
	if fields == nil {
		fields = []FieldError{}
	}
	JSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message, FieldErrors: fields}})
}

// JSON writes v as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
