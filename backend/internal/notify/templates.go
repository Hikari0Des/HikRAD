package notify

// Per-event trilingual templates (FR-162, migration 0626). Append-only and
// versioned: editing inserts the NEXT version, never rewrites history, so a
// notification_outbox row's template_version always identifies the exact
// text that produced it (gate item 6). render() is intentionally a plain
// {{key}} substitution, not a general template language — templates are
// operator-edited free text, and a substitution-only engine has no
// injection surface to worry about.

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Frozen event keys (must match notification_rules.event_key and the
// notification_templates seed in migration 0626).
const (
	EventRenewalReminder     = "renewal_reminder"
	EventExpiryNotice        = "expiry_notice"
	EventQuotaWarning        = "quota_warning"
	EventPaymentReceipt      = "payment_receipt"
	EventPlanRenewed         = "plan_renewed"
	EventPaymentTicketState  = "payment_ticket_state"
	EventStaffAlert          = "staff_alert"
)

// Template is one current-or-historical notification_templates row.
type Template struct {
	ID        string
	EventKey  string
	Locale    string
	Version   int
	Subject   string
	Body      string
	IsDefault bool
	CreatedAt string
	CreatedBy string
}

// currentTemplate returns the highest-version row for (eventKey, locale).
// Falls back to English when the requested locale has no rows yet (a
// template created only in en before ar/ku were filled in) — mirrors the
// WhatsApp catalog's own English-fallback rule so a missing translation
// never means a dropped message.
func currentTemplate(ctx context.Context, db *pgxpool.Pool, eventKey, locale string) (Template, error) {
	t, err := scanTemplateRow(db.QueryRow(ctx, `
		SELECT id::text, event_key, locale, version, subject, body, is_default, created_at::text, COALESCE(created_by::text,'')
		  FROM notification_templates WHERE event_key = $1 AND locale = $2
		 ORDER BY version DESC LIMIT 1`, eventKey, locale))
	if errors.Is(err, pgx.ErrNoRows) && locale != "en" {
		return currentTemplate(ctx, db, eventKey, "en")
	}
	return t, err
}

func scanTemplateRow(row pgx.Row) (Template, error) {
	var t Template
	err := row.Scan(&t.ID, &t.EventKey, &t.Locale, &t.Version, &t.Subject, &t.Body, &t.IsDefault, &t.CreatedAt, &t.CreatedBy)
	return t, err
}

// listCurrentTemplates returns the current (max-version) row for every
// (event, locale) pair — the Messages tab's table.
func listCurrentTemplates(ctx context.Context, db *pgxpool.Pool) ([]Template, error) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ON (event_key, locale)
		       id::text, event_key, locale, version, subject, body, is_default, created_at::text, COALESCE(created_by::text,'')
		  FROM notification_templates
		 ORDER BY event_key, locale, version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t, err := scanTemplateRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// listTemplateHistory returns every version for one (event, locale) pair,
// newest first — what the Messages tab's version history view reads.
func listTemplateHistory(ctx context.Context, db *pgxpool.Pool, eventKey, locale string) ([]Template, error) {
	rows, err := db.Query(ctx, `
		SELECT id::text, event_key, locale, version, subject, body, is_default, created_at::text, COALESCE(created_by::text,'')
		  FROM notification_templates WHERE event_key = $1 AND locale = $2
		 ORDER BY version DESC`, eventKey, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t, err := scanTemplateRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// createTemplateVersion inserts the next version for (eventKey, locale) —
// the ONLY way a template's text ever changes (binding: append-only).
func createTemplateVersion(ctx context.Context, db *pgxpool.Pool, eventKey, locale, subject, body, createdBy string) (Template, error) {
	var t Template
	err := db.QueryRow(ctx, `
		INSERT INTO notification_templates (event_key, locale, version, subject, body, is_default, created_by)
		SELECT $1, $2, COALESCE(MAX(version), 0) + 1, $3, $4, false, NULLIF($5,'')::uuid
		  FROM notification_templates WHERE event_key = $1 AND locale = $2
		RETURNING id::text, event_key, locale, version, subject, body, is_default, created_at::text, COALESCE(created_by::text,'')`,
		eventKey, locale, subject, body, createdBy,
	).Scan(&t.ID, &t.EventKey, &t.Locale, &t.Version, &t.Subject, &t.Body, &t.IsDefault, &t.CreatedAt, &t.CreatedBy)
	return t, err
}

// render substitutes {{key}} tokens in body with params[key], leaving any
// unmatched token as literal text (visible, not silently dropped — an
// operator who mistypes a placeholder sees it in the outbox preview).
func render(body string, params map[string]string) string {
	pairs := make([]string, 0, len(params)*2)
	for k, v := range params {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(body)
}
