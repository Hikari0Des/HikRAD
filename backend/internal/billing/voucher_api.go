package billing

// HTTP + internal seams for vouchers (FR-22, C3). Generation returns the plaintext
// codes as a CSV body (the only place they exist); the redeem endpoint drives the
// single renewal path; B's hotspot login redeems through the VoucherAuthenticator.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
)

type batchInput struct {
	ProfileID string     `json:"profile_id"`
	Count     int        `json:"count"`
	Prefix    string     `json:"prefix"`
	ExpiresAt *time.Time `json:"expires_at"`
	// CodeLength is the total printed length including the prefix; 0 means the
	// FR-22.1 minimum (10). The random part never drops below 8 characters, so
	// a long prefix can push the actual length past this value.
	CodeLength int `json:"code_length"`
}

func (in *batchInput) validate() []httpapi.FieldError {
	var fe []httpapi.FieldError
	if in.ProfileID == "" {
		fe = append(fe, httpapi.FieldError{Field: "profile_id", Message: "this field is required"})
	}
	if in.Count < 1 || in.Count > 10000 {
		fe = append(fe, httpapi.FieldError{Field: "count", Message: "must be between 1 and 10000"})
	}
	if len(in.Prefix) > 12 {
		fe = append(fe, httpapi.FieldError{Field: "prefix", Message: "must be at most 12 characters"})
	}
	if in.CodeLength != 0 && (in.CodeLength < minCodeLen || in.CodeLength > maxCodeLen) {
		fe = append(fe, httpapi.FieldError{Field: "code_length",
			Message: fmt.Sprintf("must be between %d and %d", minCodeLen, maxCodeLen)})
	}
	return fe
}

// createBatchHandler generates a batch and streams the plaintext codes as CSV
// (FR-22.1). The download is the ONLY time codes exist in plaintext.
func (m *Module) createBatchHandler(w http.ResponseWriter, r *http.Request) {
	var in batchInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.validate(); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	enforce := m.enforceBalanceFor(r.Context(), mgr)
	batchID, codes, err := m.generateBatch(r.Context(), in, managerID(mgr), enforce)
	switch {
	case errors.Is(err, errNoProfile):
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_profile", "profile not found")
		return
	case errors.Is(err, errProfileArchived):
		httpapi.Error(w, http.StatusUnprocessableEntity, "profile_archived", "the selected profile is archived")
		return
	case errors.Is(err, errInsufficientFunds):
		httpapi.Error(w, http.StatusUnprocessableEntity, "insufficient_balance", "billing.error.insufficient_balance")
		return
	case err != nil:
		m.internalError(w, "generate batch", err)
		return
	}
	_ = auth.Audit(r.Context(), "voucher.batch.create", "voucher_batch", batchID, nil, map[string]any{
		"profile_id": in.ProfileID, "count": in.Count, "prefix": strings.ToUpper(in.Prefix),
	})

	// CSV body: header + one code per line. Content-Disposition prompts a download.
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vouchers-%s.csv"`, batchID))
	w.Header().Set("X-Batch-Id", batchID)
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte("code\n"))
	for _, c := range codes {
		_, _ = w.Write([]byte(c + "\n"))
	}
}

type redeemRequest struct {
	Code         string `json:"code"`
	SubscriberID string `json:"subscriber_id"`
}

// redeemHandler is the operator redeem path (FR-22.3): validate + single-use →
// renewal for the target subscriber. Scoping: the redeeming manager needs
// visibility of the subscriber.
func (m *Module) redeemHandler(w http.ResponseWriter, r *http.Request) {
	var in redeemRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Code == "" || in.SubscriberID == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "code", Message: "code and subscriber_id are required"})
		return
	}
	if !m.subscriberVisible(r.Context(), in.SubscriberID, auth.ScopeFilter(r.Context())) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	res, outcome, err := m.redeemVoucher(r.Context(), in.Code, in.SubscriberID, managerID(mgr))
	if err != nil {
		if m.writeRenewError(w, err) && !errors.Is(err, errNoSubscriber) {
			return
		}
		m.internalError(w, "redeem", err)
		return
	}
	if code, msg, bad := redeemErrorFor(outcome); bad {
		httpapi.Error(w, http.StatusUnprocessableEntity, code, msg)
		return
	}
	_ = auth.Audit(r.Context(), "voucher.redeem", "subscriber", in.SubscriberID, nil, map[string]any{
		"ledger_tx_id": res.LedgerTxID, "receipt_no": res.ReceiptNo, "coa_result": res.CoAResult,
	})
	httpapi.JSON(w, http.StatusOK, res)
}

// redeemErrorFor maps a non-OK redeem outcome to a C2 error; bad=false for OK.
func redeemErrorFor(o redeemOutcome) (code, msg string, bad bool) {
	switch o {
	case redeemOK:
		return "", "", false
	case redeemUsed:
		return "voucher_used", "billing.error.voucher_used", true
	case redeemExpired:
		return "voucher_expired", "billing.error.voucher_expired", true
	case redeemBatchVoid:
		return "voucher_void", "billing.error.voucher_void", true
	default:
		return "voucher_invalid", "billing.error.voucher_invalid", true
	}
}

// --- Batch list + void ------------------------------------------------------

type batchSummary struct {
	ID           string     `json:"id"`
	ProfileID    string     `json:"profile_id"`
	Prefix       string     `json:"prefix"`
	Count        int        `json:"count"`
	UnitPriceIQD int64      `json:"unit_price_iqd"`
	State        string     `json:"state"`
	ExpiresAt    *time.Time `json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
	Used         int        `json:"used"`
	Unused       int        `json:"unused"`
	Void         int        `json:"void"`
	Expired      int        `json:"expired"`
}

func (m *Module) listBatchesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT b.id::text, b.profile_id::text, b.prefix, b.count, b.unit_price_iqd, b.state,
		        b.expires_at, b.created_at,
		        count(*) FILTER (WHERE v.state = 'used')                              AS used,
		        count(*) FILTER (WHERE v.state = 'unused'
		                          AND (b.expires_at IS NULL OR b.expires_at > now())) AS unused,
		        count(*) FILTER (WHERE v.state = 'void')                              AS voided,
		        count(*) FILTER (WHERE v.state = 'unused'
		                          AND b.expires_at IS NOT NULL AND b.expires_at <= now()) AS expired
		   FROM voucher_batches b LEFT JOIN vouchers v ON v.batch_id = b.id
		  GROUP BY b.id
		  ORDER BY b.created_at DESC
		  LIMIT 200`)
	if err != nil {
		m.internalError(w, "list batches", err)
		return
	}
	defer rows.Close()
	items := []batchSummary{}
	for rows.Next() {
		var b batchSummary
		if err := rows.Scan(&b.ID, &b.ProfileID, &b.Prefix, &b.Count, &b.UnitPriceIQD, &b.State,
			&b.ExpiresAt, &b.CreatedAt, &b.Used, &b.Unused, &b.Void, &b.Expired); err != nil {
			m.internalError(w, "scan batch", err)
			return
		}
		b.CreatedAt = b.CreatedAt.UTC()
		items = append(items, b)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (m *Module) batchDetailHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := m.db.Query(r.Context(),
		`SELECT v.id::text, v.state, v.used_for_subscriber_id::text, v.used_at
		   FROM vouchers v WHERE v.batch_id = $1::uuid ORDER BY v.id`, id)
	if err != nil {
		m.internalError(w, "batch detail", err)
		return
	}
	defer rows.Close()
	type codeRow struct {
		ID           string     `json:"id"`
		State        string     `json:"state"`
		SubscriberID *string    `json:"used_for_subscriber_id"`
		UsedAt       *time.Time `json:"used_at"`
	}
	items := []codeRow{}
	for rows.Next() {
		var c codeRow
		if err := rows.Scan(&c.ID, &c.State, &c.SubscriberID, &c.UsedAt); err != nil {
			m.internalError(w, "scan code", err)
			return
		}
		items = append(items, c)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// voidBatchHandler voids the UNUSED codes of a batch and credits the creator's
// balance back for the unused remainder (FR-22.4, C3). Used codes are untouched.
func (m *Module) voidBatchHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	tx, err := m.db.Begin(ctx)
	if err != nil {
		m.internalError(w, "void begin", err)
		return
	}
	defer tx.Rollback(ctx)

	var (
		creator   *string
		unitPrice int64
		state     string
	)
	err = tx.QueryRow(ctx,
		`SELECT creator_manager_id::text, unit_price_iqd, state
		   FROM voucher_batches WHERE id = $1::uuid FOR UPDATE`, id).
		Scan(&creator, &unitPrice, &state)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, "not_found", "batch not found")
		return
	}
	if state == "void" {
		httpapi.Error(w, http.StatusConflict, "already_void", "batch is already void")
		return
	}

	// Void the unused codes, count them for the credit.
	var voided int
	if err := tx.QueryRow(ctx,
		`WITH upd AS (
		     UPDATE vouchers SET state = 'void'
		      WHERE batch_id = $1::uuid AND state = 'unused'
		  RETURNING 1)
		 SELECT count(*) FROM upd`, id).Scan(&voided); err != nil {
		m.internalError(w, "void codes", err)
		return
	}

	creditTx := ""
	credit := unitPrice * int64(voided)
	if credit > 0 && creator != nil && *creator != "" {
		if _, err := lockBalance(ctx, tx, *creator); err != nil {
			m.internalError(w, "void lock", err)
			return
		}
		creditTx, err = insertLedger(ctx, tx, ledgerEntry{
			Type: "adjustment", AmountIQD: credit, ActorManagerID: *creator,
			Source: "voucher", Reference: id, Note: "voucher batch void (unused credit)",
		})
		if err != nil {
			m.internalError(w, "void credit", err)
			return
		}
		if err := recomputeBalance(ctx, tx, *creator); err != nil {
			m.internalError(w, "void recompute", err)
			return
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE voucher_batches SET state = 'void', void_ledger_tx_id = NULLIF($2,'')::uuid WHERE id = $1::uuid`,
		id, creditTx); err != nil {
		m.internalError(w, "void batch", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		m.internalError(w, "void commit", err)
		return
	}
	_ = auth.Audit(ctx, "voucher.batch.void", "voucher_batch", id, nil, map[string]any{
		"voided_unused": voided, "credit_iqd": credit,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{"voided_unused": voided, "credit_iqd": credit})
}

// --- Internal redeem seam for B's hotspot login (C3) ------------------------

// voucherAuthenticator implements radius.VoucherAuthenticator so a MikroTik
// Hotspot can log a guest in with a voucher code as the username (FR-18.1). The
// seam is wired now so B never imports billing; the guest-session variant
// (binding a redeemed voucher to an ephemeral subscriber and returning its
// AuthView) is a Phase-4 portal concern. Until then this refuses cleanly (B
// rejects) — it never consumes a voucher it cannot fully honor, so no code is
// burned without granting access.
type voucherAuthenticator struct{ m *Module }

func (a *voucherAuthenticator) AuthenticateVoucher(ctx context.Context, code string) (radius.AuthView, bool, error) {
	return radius.AuthView{}, false, nil
}
