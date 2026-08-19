package notify

// httpapi registration (FR-160/FR-163): the Notification Center's six-tab
// read/CRUD surface, served by hikrad-api. The background dispatcher +
// scanner run in the separate hikrad-monitor process (run.go), same split
// monitorsvc already established for its own probe/alert loops.

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Permission strings (Notification Center + support tickets share the
// module registry pattern but not a permission namespace — tickets get
// their own in internal/support).
const (
	PermView   = "notifications.view"
	PermManage = "notifications.manage"
)

var (
	pkgDB       *pgxpool.Pool
	pkgRDB      *redis.Client
	pkgSettings platform.Settings
	pkgLog      *slog.Logger
)

type Module struct{}

func (Module) Name() string { return "notify" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	pkgDB = d.DB
	pkgRDB = d.Redis
	pkgSettings = d.Settings
	pkgLog = d.Log

	// Overview tab.
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/overview", handleOverview)

	// Channels tab. Configuration itself (credentials) is NOT a separate
	// endpoint — it is the existing GET/PUT /api/v1/settings/notifications
	// (setupapi), extended this phase with the "sms" and "whatsapp_templates"
	// fields (binding rule: no new secret storage path). This tab's own
	// endpoint is read-only health/status.
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/channels", handleListChannels)

	// Messages tab (templates).
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/templates", handleListTemplates)
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/templates/{event}/{locale}/history", handleTemplateHistory)
	r.With(auth.Require(PermManage)).Put("/api/v1/notifications/templates/{event}/{locale}", handlePutTemplate)

	// Rules & timers tab.
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/rules", handleListRules)
	r.With(auth.Require(PermManage)).Put("/api/v1/notifications/rules/{event}", handlePutRule)

	// Broadcast tab.
	r.With(auth.Require(PermManage)).Post("/api/v1/notifications/broadcast", handleBroadcast)

	// Outbox tab.
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/outbox", handleListOutbox)
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/outbox/counters", handleOutboxCounters)
	r.With(auth.Require(PermView)).Get("/api/v1/notifications/outbox/{id}", handleGetOutbox)
	r.With(auth.Require(PermManage)).Post("/api/v1/notifications/outbox/{id}/retry", handleRetryOutbox)
	r.With(auth.Require(PermManage)).Post("/api/v1/notifications/outbox/{id}/cancel", handleCancelOutbox)
}

func init() { httpapi.Add(Module{}) }

func internalErr(w http.ResponseWriter, what string, err error) {
	if pkgLog != nil {
		pkgLog.Error("notify: "+what+" failed", "error", err)
	}
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}

// handleOverview serves GET /notifications/overview — live counts plus
// each channel's configured/not-configured state (gate item 1's proof
// surface: everything, regardless of source, is one set of numbers).
func handleOverview(w http.ResponseWriter, r *http.Request) {
	c, err := outboxCounters(r.Context(), pkgDB)
	if err != nil {
		internalErr(w, "overview counters", err)
		return
	}
	statuses := channelStatuses(r.Context())
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"counters": c,
		"channels": statuses,
	})
}

type outboxItemView struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	SourceID        string    `json:"source_id,omitempty"`
	Event           string    `json:"event"`
	SubscriberID    string    `json:"subscriber_id,omitempty"`
	ManagerID       string    `json:"manager_id,omitempty"`
	Address         string    `json:"address"`
	Channel         string    `json:"channel"`
	Locale          string    `json:"locale"`
	TemplateKey     string    `json:"template_key,omitempty"`
	TemplateVersion *int      `json:"template_version,omitempty"`
	Subject         string    `json:"subject,omitempty"`
	Body            string    `json:"body"`
	IgnoreOptOut    bool      `json:"ignore_opt_out"`
	State           string    `json:"state"`
	AttemptCount    int       `json:"attempt_count"`
	LastError       string    `json:"last_error,omitempty"`
	SkipReason      string    `json:"skip_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	SentAt          *time.Time `json:"sent_at,omitempty"`
}

func toOutboxItemView(r OutboxRow) outboxItemView {
	return outboxItemView{
		ID: r.ID, Source: r.Source, SourceID: r.SourceID, Event: r.Event, SubscriberID: r.SubscriberID,
		ManagerID: r.ManagerID, Address: r.Address, Channel: r.Channel, Locale: r.Locale,
		TemplateKey: r.TemplateKey, TemplateVersion: r.TemplateVersion, Subject: r.Subject, Body: r.Body,
		IgnoreOptOut: r.IgnoreOptOut, State: r.State, AttemptCount: r.AttemptCount,
		LastError: r.LastError, SkipReason: r.SkipReason,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), SentAt: utcPtr(r.SentAt),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// handleListOutbox serves GET /notifications/outbox (FR-160's Outbox tab:
// every queued and sent message with per-message status and failure reason).
func handleListOutbox(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	f := OutboxFilter{
		State:   r.URL.Query().Get("state"),
		Channel: r.URL.Query().Get("channel"),
		Event:   r.URL.Query().Get("event"),
		Search:  r.URL.Query().Get("search"),
	}
	rows, next, err := listOutbox(r.Context(), pkgDB, f, page.Cursor, page.Limit)
	if err != nil {
		internalErr(w, "list outbox", err)
		return
	}
	items := make([]outboxItemView, 0, len(rows))
	for _, row := range rows {
		items = append(items, toOutboxItemView(row))
	}
	nextCursor := ""
	if len(next) == 2 {
		nextCursor = httpapi.EncodeCursor(next[0], next[1])
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, nextCursor))
}

func handleOutboxCounters(w http.ResponseWriter, r *http.Request) {
	c, err := outboxCounters(r.Context(), pkgDB)
	if err != nil {
		internalErr(w, "outbox counters", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, c)
}

func handleGetOutbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row, err := getOutbox(r.Context(), pkgDB, id)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no such outbox row")
		return
	}
	httpapi.JSON(w, http.StatusOK, toOutboxItemView(row))
}

func handleRetryOutbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := retryOutboxRow(r.Context(), pkgDB, id)
	if err != nil {
		internalErr(w, "retry outbox row", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusConflict, "not_retryable", "row is not in a retryable state (failed)")
		return
	}
	_ = auth.Audit(r.Context(), "notification.retry", "notification_outbox", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "queued"})
}

func handleCancelOutbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := cancelOutboxRow(r.Context(), pkgDB, id)
	if err != nil {
		internalErr(w, "cancel outbox row", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusConflict, "not_cancellable", "row already sent/resolved — only a queued row can be cancelled")
		return
	}
	_ = auth.Audit(r.Context(), "notification.cancel", "notification_outbox", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "skipped"})
}
