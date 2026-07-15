package monitorsvc

// Alert-rule CRUD (FR-36, contract C5). The rule `type` and channel enum are the
// frozen contract; validation rejects anything outside them so a bad rule can't
// silently never fire. Editing takes effect on the alert engine's next fire (it
// reloads rules per fire), so no cross-process signal is needed.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

// Frozen rule types (contract C5).
var validRuleTypes = map[string]bool{
	"nas_down": true, "nas_up": true, "device_down": true, "device_up": true,
	"radius_reject_spike": true, "acct_backlog": true, "disk_low": true,
	"expiring_digest": true, "agent_balance_low": true,
}

// Frozen channel enum (contract C5).
var validChannels = map[string]bool{
	chInApp: true, chTelegram: true, chEmail: true, chWhatsApp: true, chPush: true,
}

type alertRuleView struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Threshold  json.RawMessage `json:"threshold"`
	Channels   []string        `json:"channels"`
	Recipients json.RawMessage `json:"recipients"`
	QuietHours json.RawMessage `json:"quiet_hours"`
	CooldownS  int             `json:"cooldown_s"`
	Enabled    bool            `json:"enabled"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type alertRuleInput struct {
	Name       string          `json:"name" validate:"required,min=1,max=128"`
	Type       string          `json:"type" validate:"required"`
	Threshold  json.RawMessage `json:"threshold"`
	Channels   []string        `json:"channels" validate:"required,min=1"`
	Recipients json.RawMessage `json:"recipients"`
	QuietHours json.RawMessage `json:"quiet_hours"`
	CooldownS  *int            `json:"cooldown_s"`
	Enabled    *bool           `json:"enabled"`
}

// validateRule checks the frozen enums beyond the struct tags.
func (in alertRuleInput) fieldErrors() []httpapi.FieldError {
	var fe []httpapi.FieldError
	if !validRuleTypes[in.Type] {
		fe = append(fe, httpapi.FieldError{Field: "type", Message: "unknown rule type"})
	}
	for _, c := range in.Channels {
		if !validChannels[c] {
			fe = append(fe, httpapi.FieldError{Field: "channels", Message: "unknown channel: " + c})
		}
	}
	if in.CooldownS != nil && *in.CooldownS < 0 {
		fe = append(fe, httpapi.FieldError{Field: "cooldown_s", Message: "must be ≥ 0"})
	}
	return fe
}

func listAlertRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := pkgDB.Query(ctx,
		`SELECT id::text, name, type, threshold, channels, recipients, quiet_hours,
		        cooldown_s, enabled, created_at, updated_at
		   FROM alert_rules ORDER BY type, name`)
	if err != nil {
		internalErr(w, "list rules", err)
		return
	}
	defer rows.Close()
	items := make([]alertRuleView, 0, 32)
	for rows.Next() {
		v, err := scanRule(rows)
		if err != nil {
			internalErr(w, "scan rule", err)
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		internalErr(w, "rows rules", rows.Err())
		return
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, ""))
}

func createAlertRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in alertRuleInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.fieldErrors(); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	threshold, channels, recipients, quiet := ruleDefaults(in)
	cooldown := 300
	if in.CooldownS != nil {
		cooldown = *in.CooldownS
	}
	enabled := in.Enabled == nil || *in.Enabled

	var id string
	err := pkgDB.QueryRow(ctx,
		`INSERT INTO alert_rules (name, type, threshold, channels, recipients, quiet_hours, cooldown_s, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id::text`,
		in.Name, in.Type, threshold, channels, recipients, quiet, cooldown, enabled).Scan(&id)
	if err != nil {
		internalErr(w, "insert rule", err)
		return
	}
	_ = auth.Audit(ctx, "alert_rule.create", "alert_rule", id, nil, map[string]any{"name": in.Name, "type": in.Type})
	v, _ := readRule(ctx, id)
	httpapi.JSON(w, http.StatusCreated, v)
}

func updateAlertRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	before, err := readRule(ctx, id)
	if err == pgx.ErrNoRows {
		httpapi.Error(w, http.StatusNotFound, "not_found", "alert rule not found")
		return
	}
	if err != nil {
		internalErr(w, "load rule", err)
		return
	}
	var in alertRuleInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.fieldErrors(); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	threshold, channels, recipients, quiet := ruleDefaults(in)
	cooldown := before.CooldownS
	if in.CooldownS != nil {
		cooldown = *in.CooldownS
	}
	enabled := before.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	_, err = pkgDB.Exec(ctx,
		`UPDATE alert_rules
		    SET name=$2, type=$3, threshold=$4, channels=$5, recipients=$6,
		        quiet_hours=$7, cooldown_s=$8, enabled=$9
		  WHERE id=$1::uuid`,
		id, in.Name, in.Type, threshold, channels, recipients, quiet, cooldown, enabled)
	if err != nil {
		internalErr(w, "update rule", err)
		return
	}
	_ = auth.Audit(ctx, "alert_rule.update", "alert_rule", id,
		map[string]any{"name": before.Name, "enabled": before.Enabled},
		map[string]any{"name": in.Name, "enabled": enabled})
	v, _ := readRule(ctx, id)
	httpapi.JSON(w, http.StatusOK, v)
}

// ruleDefaults normalizes optional jsonb fields to valid defaults.
func ruleDefaults(in alertRuleInput) (threshold, channels, recipients, quiet []byte) {
	threshold = []byte(in.Threshold)
	if len(threshold) == 0 {
		threshold = []byte(`{}`)
	}
	channels, _ = json.Marshal(in.Channels)
	recipients = []byte(in.Recipients)
	if len(recipients) == 0 {
		recipients = []byte(`{}`)
	}
	quiet = []byte(in.QuietHours)
	if len(quiet) == 0 || string(quiet) == "null" {
		quiet = nil
	}
	return
}

func readRule(ctx context.Context, id string) (alertRuleView, error) {
	row := pkgDB.QueryRow(ctx,
		`SELECT id::text, name, type, threshold, channels, recipients, quiet_hours,
		        cooldown_s, enabled, created_at, updated_at
		   FROM alert_rules WHERE id=$1::uuid`, id)
	return scanRule(row)
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRule(row rowScanner) (alertRuleView, error) {
	var v alertRuleView
	var channels []byte
	var quiet []byte
	if err := row.Scan(&v.ID, &v.Name, &v.Type, &v.Threshold, &channels, &v.Recipients,
		&quiet, &v.CooldownS, &v.Enabled, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return alertRuleView{}, err
	}
	_ = json.Unmarshal(channels, &v.Channels)
	if len(quiet) > 0 {
		v.QuietHours = json.RawMessage(quiet)
	}
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	return v, nil
}
