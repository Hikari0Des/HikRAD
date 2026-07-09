package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate = func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Report JSON field names, not Go field names, in field_errors.
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if name == "-" || name == "" {
			return f.Name
		}
		return name
	})
	return v
}()

// Validate checks struct `validate` tags and returns C2 field_errors,
// or nil when the value is valid.
func Validate(v any) []FieldError {
	err := validate.Struct(v)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return []FieldError{{Field: "", Message: err.Error()}}
	}
	out := make([]FieldError, 0, len(verrs))
	for _, fe := range verrs {
		out = append(out, FieldError{Field: fe.Field(), Message: validationMessage(fe)})
	}
	return out
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "this field is required"
	case "oneof":
		return "must be one of: " + fe.Param()
	case "min":
		return "must be at least " + fe.Param()
	case "max":
		return "must be at most " + fe.Param()
	case "email":
		return "must be a valid email address"
	default:
		return "failed validation rule: " + fe.Tag()
	}
}

// Bind decodes the JSON request body into dst and validates it. On failure
// it writes the appropriate C2 error response (400 for malformed JSON, 422
// with field_errors for validation failures) and returns false.
func Bind(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return false
	}
	if fields := Validate(dst); fields != nil {
		Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fields...)
		return false
	}
	return true
}
