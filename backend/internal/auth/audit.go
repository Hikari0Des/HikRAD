package auth

// Append-only audit log write API (FR-28.3, contract C2). Every mutating
// endpoint in every module calls Audit; this package owns the single writer
// and the immutable table (migration 0112). before/after are redacted of
// secret-tagged fields (struct tag `audit:"secret"`) so passwords and shared
// secrets never land in the log.

import (
	"context"
	"encoding"
	"encoding/json"
	"reflect"
)

var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// marshalsItself reports whether t (or *t) controls its own JSON/text
// encoding, in which case redactValue must not descend into it.
func marshalsItself(t reflect.Type) bool {
	pt := reflect.PointerTo(t)
	return t.Implements(jsonMarshalerType) || pt.Implements(jsonMarshalerType) ||
		t.Implements(textMarshalerType) || pt.Implements(textMarshalerType)
}

const redacted = "[REDACTED]"

// Audit records one action. actor/IP/UA come from the Manager in ctx (set by
// Require); a context with no Manager (e.g. pre-login events) records a null
// actor — use AuditActor for those. entityID may be empty. before/after may be
// nil. It is best-effort: a failure is logged, never surfaced to the caller,
// because a completed mutation must not be rolled back by a logging failure —
// but the write is expected to succeed and CI asserts rows exist.
func Audit(ctx context.Context, action, entityType, entityID string, before, after any) error {
	var actorID, ip, ua string
	if m, ok := ManagerFrom(ctx); ok {
		actorID, ip, ua = m.ID, m.IP, m.UA
	}
	return auditWrite(ctx, actorID, ip, ua, action, entityType, entityID, before, after)
}

// AuditActor records an action for an explicit actor/IP/UA, for events that
// happen before a Manager is in context (login success/failure, refresh
// reuse). actorID may be "" (unknown user) → null actor_id.
func AuditActor(ctx context.Context, actorID, ip, ua, action, entityType, entityID string, before, after any) error {
	return auditWrite(ctx, actorID, ip, ua, action, entityType, entityID, before, after)
}

func auditWrite(ctx context.Context, actorID, ip, ua, action, entityType, entityID string, before, after any) error {
	if svc == nil || svc.db == nil {
		return nil
	}
	beforeJSON, err := redactJSON(before)
	if err != nil {
		svc.log.Error("audit: marshal before failed", "error", err, "action", action)
		return err
	}
	afterJSON, err := redactJSON(after)
	if err != nil {
		svc.log.Error("audit: marshal after failed", "error", err, "action", action)
		return err
	}
	var actor *string
	if actorID != "" {
		actor = &actorID
	}
	_, err = svc.db.Exec(ctx,
		`INSERT INTO audit_log (actor_id, action, entity_type, entity_id, before, after, ip, ua)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)`,
		actor, action, entityType, entityID, beforeJSON, afterJSON, ip, ua)
	if err != nil {
		svc.log.Error("audit: insert failed", "error", err, "action", action, "actor", actorID)
	}
	return err
}

// redactJSON marshals v to json.RawMessage after replacing every field tagged
// `audit:"secret"` with a placeholder. Returns nil for a nil input (stored as
// SQL NULL).
func redactJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	cleaned := redactValue(reflect.ValueOf(v))
	return json.Marshal(cleaned)
}

// redactValue walks a value, returning a JSON-marshalable copy with
// secret-tagged struct fields replaced. It handles structs (and pointers to
// them), slices/arrays and maps recursively; everything else passes through.
func redactValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return redactValue(v.Elem())
	case reflect.Struct:
		// Types that control their own JSON (time.Time, json.RawMessage,
		// uuids, …) must not be field-walked — that would emit `{}` for their
		// unexported internals. Defer to their marshaler.
		if marshalsItself(v.Type()) {
			return v.Interface()
		}
		out := make(map[string]any, v.NumField())
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" { // unexported
				continue
			}
			name, omit := jsonFieldName(f)
			if name == "-" {
				continue
			}
			fv := v.Field(i)
			if f.Tag.Get("audit") == "secret" {
				// Only surface the key when there is a value to redact.
				if !isEmptyValue(fv) {
					out[name] = redacted
				} else if !omit {
					out[name] = nil
				}
				continue
			}
			if omit && isEmptyValue(fv) {
				continue
			}
			out[name] = redactValue(fv)
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		// []byte is base64-encoded by encoding/json; keep that behavior.
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return v.Interface()
		}
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = redactValue(v.Index(i))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		out := make(map[string]any, v.Len())
		for _, k := range v.MapKeys() {
			out[stringifyKey(k)] = redactValue(v.MapIndex(k))
		}
		return out
	default:
		return v.Interface()
	}
}

func jsonFieldName(f reflect.StructField) (name string, omitempty bool) {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name, false
	}
	parts := splitComma(tag)
	name = parts[0]
	if name == "" {
		name = f.Name
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func stringifyKey(k reflect.Value) string {
	if k.Kind() == reflect.String {
		return k.String()
	}
	b, _ := json.Marshal(k.Interface())
	return string(b)
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}
