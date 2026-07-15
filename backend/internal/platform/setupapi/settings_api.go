package setupapi

// GET/PUT /api/v1/settings/{group} (FR-53.1/53.2). Settings are a flat
// key-value table (`settings`, migration 0010); a "group" here is just the
// dot-prefix convention every module already reads/writes directly
// (billing.*, locale.*, notifications.*) — this endpoint is a generic
// reader/writer over that same convention, not a second source of truth, so
// PUT /api/v1/settings/billing {"renewal_anchor":"from_now"} lands on the
// exact key internal/billing/settings.go reads.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform/crypto"
)

// settingsGroups declares every v1 group (FR-53.2) and its allowed field
// names. A PUT with a field not in this list is rejected (422) rather than
// silently creating an unread key — schema-validated per FR-53.1.
var settingsGroups = map[string][]string{
	"locale":         {"timezone", "currency", "date_format", "language"},
	"branding":       {"name", "logo_url", "primary_color", "secondary_color"},
	"notifications":  {"smtp", "telegram", "whatsapp"},
	"billing":        {"renewal_anchor", "admin_balance_bypass", "receipt_prefix", "receipt_branding", "voucher_prefix", "receipt_numerals"},
	"backups":        {"schedule_hour", "retention_count", "path"},
	"data_retention": {"raw_months", "rollup_years"},
	"remote_access":  {"enabled", "token"}, // token is write-only; see below
	"card_payments":  {"types", "reject_cooldown_days"},
}

// remoteAccessTokenEncKey is where the encrypted tunnel token actually lives;
// "remote_access.token" itself is never written (NFR-4.3: secrets at rest
// under the one AES-GCM envelope, and a plaintext token must never round-trip
// through GET).
const remoteAccessTokenEncKey = "remote_access.token_enc"

func groupFields(group string) ([]string, bool) {
	f, ok := settingsGroups[group]
	return f, ok
}

func readBranding(ctx context.Context) map[string]json.RawMessage {
	return readGroup(ctx, "branding")
}

// readGroup fetches every declared field of group from the settings service,
// omitting fields with no stored value.
func readGroup(ctx context.Context, group string) map[string]json.RawMessage {
	fields, _ := groupFields(group)
	out := make(map[string]json.RawMessage, len(fields))
	for _, f := range fields {
		if group == "remote_access" && f == "token" {
			_, err := svc.settings.GetRaw(ctx, remoteAccessTokenEncKey)
			b, _ := json.Marshal(err == nil)
			out["token_set"] = b
			continue
		}
		raw, err := svc.settings.GetRaw(ctx, group+"."+f)
		if err != nil {
			continue
		}
		out[f] = raw
	}
	return out
}

func getSettingsGroupHandler(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	if _, ok := groupFields(group); !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "unknown settings group")
		return
	}
	httpapi.JSON(w, http.StatusOK, readGroup(r.Context(), group))
}

func putSettingsGroupHandler(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	fields, ok := groupFields(group)
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "unknown settings group")
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	allowed := make(map[string]bool, len(fields))
	for _, f := range fields {
		allowed[f] = true
	}
	for f := range body {
		if !allowed[f] {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "unknown field for this settings group",
				httpapi.FieldError{Field: f, Message: "not a valid field for group " + group})
			return
		}
	}
	if fe := validateDataRetentionFloors(group, body); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "value is below the required retention floor", *fe)
		return
	}

	ctx := r.Context()
	for f, raw := range body {
		if group == "remote_access" && f == "token" {
			if err := setRemoteAccessToken(ctx, raw); err != nil {
				svc.log.Error("settings: encrypt remote_access.token failed", "error", err)
				httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			continue
		}
		if err := svc.settings.Set(ctx, group+"."+f, raw); err != nil {
			svc.log.Error("settings: set failed", "error", err, "key", group+"."+f)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
	}
	_ = auth.Audit(ctx, "settings.update", "settings", group, nil, redactedGroupBody(group, body))
	httpapi.JSON(w, http.StatusOK, readGroup(ctx, group))
}

// dataRetentionFloors are the sub-PRD 03 FR-33 minimums (raw sessions ≥ 12
// months, rollups ≥ 3 years): an admin can raise them for a bigger disk, but
// the panel must never accept a value that would let the accounting pipeline
// itself violate the retention contract other modules assume holds.
var dataRetentionFloors = map[string]int{"raw_months": 12, "rollup_years": 3}

func validateDataRetentionFloors(group string, body map[string]json.RawMessage) *httpapi.FieldError {
	if group != "data_retention" {
		return nil
	}
	for field, floor := range dataRetentionFloors {
		raw, ok := body[field]
		if !ok {
			continue
		}
		var n int
		if err := json.Unmarshal(raw, &n); err != nil || n < floor {
			return &httpapi.FieldError{Field: field, Message: fmt.Sprintf("must be at least %d", floor)}
		}
	}
	return nil
}

// setRemoteAccessToken stores the tunnel token AES-GCM sealed (NFR-4.3);
// raw is the JSON string the client sent (e.g. `"abcd1234"`).
func setRemoteAccessToken(ctx context.Context, raw json.RawMessage) error {
	var token string
	if err := json.Unmarshal(raw, &token); err != nil {
		return err
	}
	if token == "" {
		return svc.settings.Set(ctx, remoteAccessTokenEncKey, "")
	}
	enc, err := crypto.Encrypt([]byte(token))
	if err != nil {
		return err
	}
	return svc.settings.Set(ctx, remoteAccessTokenEncKey, enc)
}

// redactedGroupBody keeps secrets (the tunnel token, SMTP/Telegram/WhatsApp
// credentials embedded in the notifications group) out of the audit log,
// mirroring auth.Audit's `audit:"secret"` convention for the one place this
// endpoint accepts free-form JSON it can't tag a struct field on.
func redactedGroupBody(group string, body map[string]json.RawMessage) map[string]string {
	out := make(map[string]string, len(body))
	for f := range body {
		if group == "remote_access" && f == "token" {
			out[f] = "[REDACTED]"
			continue
		}
		if group == "notifications" {
			out[f] = "[REDACTED]" // smtp/telegram/whatsapp all carry credentials
			continue
		}
		out[f] = "changed"
	}
	return out
}
