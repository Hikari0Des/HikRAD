package radius

// CoA Outbox operator surface (FR-152): six counters, status/action filters,
// username/NAS/reason search, per-row retry/cancel/inspect. Permission-gated
// separately from nas.* — this is an operations screen, not NAS config.

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

// PermCoAOutboxView/Manage are the outbox's own permission strings (roles.go
// catalog), separate from nas.* since this is an operations surface over
// every NAS at once, not a per-router config screen.
const (
	PermCoAOutboxView   = "coa_outbox.view"
	PermCoAOutboxManage = "coa_outbox.manage"
)

type outboxItemView struct {
	ID             string     `json:"id"`
	SubscriberID   string     `json:"subscriber_id,omitempty"`
	NASID          string     `json:"nas_id"`
	NASName        string     `json:"nas_name"`
	AcctSessionID  string     `json:"acct_session_id"`
	Username       string     `json:"username"`
	FramedIP       string     `json:"framed_ip"`
	Service        string     `json:"service"`
	Action         string     `json:"action"`
	CurrentOp      string     `json:"current_op"`
	Attributes     []wireAttrView `json:"attributes"`
	Reason         string     `json:"reason"`
	RequestedBy    string     `json:"requested_by,omitempty"`
	State          string     `json:"state"`
	AttemptCount   int        `json:"attempt_count"`
	LastError      string     `json:"last_error,omitempty"`
	LastOutcome    string     `json:"last_outcome,omitempty"`
	FallbackUsed   bool       `json:"fallback_used"`
	FallbackAt     *time.Time `json:"fallback_at,omitempty"`
	FallbackReason string     `json:"fallback_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	DoneAt         *time.Time `json:"done_at,omitempty"`
}

// wireAttrView is the intended-attribute-set shape shown in the list/inspect
// (vendor.Attr's abstract intent/value — the per-attempt wire-level capture
// is a separate, richer view, see outboxAttemptView).
type wireAttrView struct {
	Intent string `json:"intent"`
	Value  string `json:"value"`
}

func toOutboxItemView(r OutboxRow, nasNamesByID map[string]string) outboxItemView {
	attrs := make([]wireAttrView, 0, len(r.Attributes))
	for _, a := range r.Attributes {
		attrs = append(attrs, wireAttrView{Intent: a.Intent, Value: a.Value})
	}
	return outboxItemView{
		ID: r.ID, SubscriberID: r.SubscriberID, NASID: r.NASID, NASName: nasNamesByID[r.NASID],
		AcctSessionID: r.AcctSessionID, Username: r.Username, FramedIP: r.FramedIP, Service: r.Service,
		Action: string(r.Action), CurrentOp: string(r.CurrentOp), Attributes: attrs, Reason: r.Reason,
		RequestedBy: r.RequestedBy, State: string(r.State), AttemptCount: r.AttemptCount,
		LastError: r.LastError, LastOutcome: r.LastOutcome,
		FallbackUsed: r.FallbackUsed, FallbackAt: r.FallbackAt, FallbackReason: r.FallbackReason,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
		SentAt: utcPtr(r.SentAt), DoneAt: utcPtr(r.DoneAt),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// listCoAOutboxHandler serves GET /coa-outbox (FR-152 filters/search/pagination).
func (m *module) listCoAOutboxHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	f := outboxFilter{
		State:  r.URL.Query().Get("state"),
		Action: r.URL.Query().Get("action"),
		Search: r.URL.Query().Get("search"),
	}
	rows, next, err := listOutbox(r.Context(), m.db, f, page.Cursor, page.Limit)
	if err != nil {
		m.internal(w, "list coa outbox", err)
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.NASID)
	}
	names, err := nasNames(r.Context(), m.db, ids)
	if err != nil {
		m.internal(w, "resolve nas names", err)
		return
	}
	items := make([]outboxItemView, 0, len(rows))
	for _, row := range rows {
		items = append(items, toOutboxItemView(row, names))
	}
	nextCursor := ""
	if len(next) == 2 {
		nextCursor = httpapi.EncodeCursor(next[0], next[1])
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, nextCursor))
}

// countersCoAOutboxHandler serves GET /coa-outbox/counters — the six FR-152
// counters plus the 24h failure count v4-12's dashboard panel will read.
func (m *module) countersCoAOutboxHandler(w http.ResponseWriter, r *http.Request) {
	c, err := outboxCounters(r.Context(), m.db)
	if err != nil {
		m.internal(w, "coa outbox counters", err)
		return
	}
	failed24h, err := recentFailureCount(r.Context(), m.db, time.Now().Add(-24*time.Hour))
	if err != nil {
		m.internal(w, "coa outbox 24h failures", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"pending": c.Pending, "sent": c.Sent, "nack": c.Nack, "done": c.Done, "failed": c.Failed,
		"total": c.Total, "failed_24h": failed24h,
	})
}

type outboxAttemptView struct {
	AttemptNo      int        `json:"attempt_no"`
	Op             string     `json:"op"`
	Outcome        string     `json:"outcome"`
	Error          string     `json:"error,omitempty"`
	WireAttributes []WireAttr `json:"wire_attributes"`
	AttemptedAt    time.Time  `json:"attempted_at"`
}

type outboxDetailView struct {
	outboxItemView
	Attempts []outboxAttemptView `json:"attempts"`
}

// getCoAOutboxHandler serves GET /coa-outbox/{id} — the inspect view (gate
// item 6): the row plus every dispatch attempt's exact wire-level attributes,
// stored as sent and never regenerated here.
func (m *module) getCoAOutboxHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row, err := getOutbox(r.Context(), m.db, id)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no such coa outbox row")
		return
	}
	names, err := nasNames(r.Context(), m.db, []string{row.NASID})
	if err != nil {
		m.internal(w, "resolve nas name", err)
		return
	}
	attempts, err := listAttempts(r.Context(), m.db, id)
	if err != nil {
		m.internal(w, "list coa attempts", err)
		return
	}
	av := make([]outboxAttemptView, 0, len(attempts))
	for _, a := range attempts {
		av = append(av, outboxAttemptView{
			AttemptNo: a.AttemptNo, Op: string(a.Op), Outcome: a.Outcome, Error: a.Error,
			WireAttributes: a.WireAttributes, AttemptedAt: a.AttemptedAt.UTC(),
		})
	}
	httpapi.JSON(w, http.StatusOK, outboxDetailView{
		outboxItemView: toOutboxItemView(row, names), Attempts: av,
	})
}

// retryCoAOutboxHandler serves POST /coa-outbox/{id}/retry (gate item 5).
func (m *module) retryCoAOutboxHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := retryOutboxRow(r.Context(), m.db, id)
	if err != nil {
		m.internal(w, "retry coa outbox row", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusConflict, "not_retryable", "row is not in a retryable state (nack/failed)")
		return
	}
	_ = auth.Audit(r.Context(), "coa_outbox.retry", "coa_outbox", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "pending"})
}

// cancelCoAOutboxHandler serves POST /coa-outbox/{id}/cancel (gate item 5:
// "cancel stops an unsent row and cannot cancel a sent one").
func (m *module) cancelCoAOutboxHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := cancelOutboxRow(r.Context(), m.db, id)
	if err != nil {
		m.internal(w, "cancel coa outbox row", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusConflict, "not_cancellable", "row already sent/resolved — only a pending row can be cancelled")
		return
	}
	_ = auth.Audit(r.Context(), "coa_outbox.cancel", "coa_outbox", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "failed"})
}
