package portalapi

// Portal notification-channel preferences (FR-170, consuming v4-10's
// subscriber_channel_opt_ins table, migration 0627). A subscriber-safe
// account setting — same class as password/contact — so both the read and
// write sides are gated behind requireFullScope (FR-167 gate item 9): an
// auto-logged-in reduced-scope session may not silence or redirect where
// their renewal reminders and payment receipts go.
//
// Only sms/email have an explicit opt-in row (absence = opted-in, matching
// notify/enqueue.go's own model); whatsapp_opt_in is its own column on
// subscribers (a Meta consent requirement, FR-55.3 — always explicit, never
// defaulted true) — this endpoint reads/writes both under one shape rather
// than exposing two different mechanisms to the client.

import (
	"context"
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type notificationPrefs struct {
	SMS      bool `json:"sms"`
	Email    bool `json:"email"`
	WhatsApp bool `json:"whatsapp"`
}

func loadNotificationPrefs(ctx context.Context, m *Module, subscriberID string) (notificationPrefs, error) {
	p := notificationPrefs{SMS: true, Email: true} // absence of a row = opted-in (enqueue.go's own rule)
	rows, err := m.db.Query(ctx,
		`SELECT channel, opted_in FROM subscriber_channel_opt_ins WHERE subscriber_id = $1::uuid`, subscriberID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var channel string
		var optedIn bool
		if err := rows.Scan(&channel, &optedIn); err != nil {
			return p, err
		}
		switch channel {
		case "sms":
			p.SMS = optedIn
		case "email":
			p.Email = optedIn
		}
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	if err := m.db.QueryRow(ctx, `SELECT whatsapp_opt_in FROM subscribers WHERE id = $1::uuid`, subscriberID).Scan(&p.WhatsApp); err != nil {
		return p, err
	}
	return p, nil
}

func (m *Module) notificationPrefsHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	p, err := loadNotificationPrefs(r.Context(), m, sub.ID)
	if err != nil {
		m.internalError(w, "load notification prefs", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p)
}

func (m *Module) updateNotificationPrefsHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	var in notificationPrefs
	if !httpapi.Bind(w, r, &in) {
		return
	}
	ctx := r.Context()
	for _, ch := range []struct {
		name    string
		optedIn bool
	}{{"sms", in.SMS}, {"email", in.Email}} {
		if _, err := m.db.Exec(ctx,
			`INSERT INTO subscriber_channel_opt_ins (subscriber_id, channel, opted_in)
			 VALUES ($1::uuid, $2, $3)
			 ON CONFLICT (subscriber_id, channel) DO UPDATE SET opted_in = EXCLUDED.opted_in, updated_at = now()`,
			sub.ID, ch.name, ch.optedIn); err != nil {
			m.internalError(w, "update notification prefs", err)
			return
		}
	}
	if _, err := m.db.Exec(ctx, `UPDATE subscribers SET whatsapp_opt_in = $2, updated_at = now() WHERE id = $1::uuid`,
		sub.ID, in.WhatsApp); err != nil {
		m.internalError(w, "update whatsapp opt-in", err)
		return
	}
	_ = auth.AuditActor(ctx, sub.ID, sub.IP, sub.UA, "portal.notification_prefs.update", "subscriber", sub.ID, nil, in)
	httpapi.JSON(w, http.StatusOK, in)
}
