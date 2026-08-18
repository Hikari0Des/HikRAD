package subscribers

// Batch / numbered creation (v4-5, FR-133). Structurally mirrors bulk.go: the
// same in-memory job registry, the same per-row-independent execution loop
// (one collision does not roll back the rest), the same detached-context
// audit writes. What's new here is generation (usernames from a prefix +
// numeric range, not a filter) and the licence-cap pre-check, which — unlike
// every per-row failure below it — must refuse the WHOLE batch before a
// single row is written.

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/radius"
)

const (
	invoiceEach  = "each"
	invoiceBatch = "batch"
	invoiceNone  = "none"
)

// batchInput is the C7-D batch-creation body.
type batchInput struct {
	Prefix     string `json:"prefix"`
	StartIndex int    `json:"start_index"`
	Count      int    `json:"count"`
	// DigitPadding: total digit width of the numeric suffix. 0 = the natural
	// width of the LARGEST index in the batch, so every username in one batch
	// is the same length (user001..user200, not user1..user200).
	DigitPadding int `json:"digit_padding"`
	// PasswordMode: "shared" (SharedPassword for every row) or "generated" (a
	// distinct random password per row, returned once in the job result — the
	// same "exists only at creation time" posture voucher codes follow).
	PasswordMode   string  `json:"password_mode"`
	SharedPassword string  `json:"shared_password"`
	ProfileID      string  `json:"profile_id"`
	ExpiresAt      *string `json:"expires_at"` // RFC3339; only meaningful for invoicing "none"
	AutoRenew      bool    `json:"auto_renew"`
	OwnerManagerID string  `json:"owner_manager_id"`
	ServiceType    string  `json:"service_type"`
	// Invoicing: "each" (a real FR-19 renewal + receipt per subscriber) |
	// "batch" (one aggregate ledger entry for the whole run) | "none" (pure
	// provisioning, no money moves — e.g. trial/complimentary accounts).
	Invoicing string `json:"invoicing"`
	// DryRun returns the exact usernames that would be created and any
	// collisions, writing nothing (gate item 3: "dry-run matches the result
	// exactly").
	DryRun bool `json:"dry_run"`
}

func (in *batchInput) validate() []httpapi.FieldError {
	var fe []httpapi.FieldError
	add := func(f, m string) { fe = append(fe, httpapi.FieldError{Field: f, Message: m}) }

	if in.Count < 1 || in.Count > 10000 {
		add("count", "must be between 1 and 10000")
	}
	if in.StartIndex < 0 {
		add("start_index", "must not be negative")
	}
	if in.DigitPadding < 0 || in.DigitPadding > 10 {
		add("digit_padding", "must be between 0 and 10")
	}
	if in.ProfileID == "" {
		add("profile_id", "this field is required")
	}
	switch in.PasswordMode {
	case "", "shared", "generated":
	default:
		add("password_mode", "must be one of: shared generated")
	}
	if in.PasswordMode != "generated" && strings.TrimSpace(in.SharedPassword) == "" {
		add("shared_password", "required unless password_mode is 'generated'")
	}
	switch in.ServiceType {
	case "":
		in.ServiceType = "pppoe"
	case "pppoe", "hotspot", "dual":
	default:
		add("service_type", "must be one of: pppoe hotspot dual")
	}
	switch in.Invoicing {
	case "":
		in.Invoicing = invoiceNone
	case invoiceEach, invoiceBatch, invoiceNone:
	default:
		add("invoicing", "must be one of: each batch none")
	}
	return fe
}

// usernames computes the exact ordered username list a batch will create —
// the SAME function dry-run and execute both call, so "dry-run matches the
// result exactly" (gate item 3) holds by construction, not by keeping two
// generators in sync by hand.
func (in batchInput) usernames() []string {
	width := in.DigitPadding
	if width == 0 {
		width = len(fmt.Sprintf("%d", in.StartIndex+in.Count-1))
	}
	out := make([]string, in.Count)
	for i := 0; i < in.Count; i++ {
		out[i] = fmt.Sprintf("%s%0*d", in.Prefix, width, in.StartIndex+i)
	}
	return out
}

// batchDryRunResponse is gate item 3's proof shape.
type batchDryRunResponse struct {
	Usernames  []string `json:"usernames"`
	Collisions []string `json:"collisions"`
	Total      int      `json:"total"`
	CapBlocked bool     `json:"cap_blocked"`
	CapMessage string   `json:"cap_message,omitempty"`
}

func (m *Module) batchHandler(w http.ResponseWriter, r *http.Request) {
	var in batchInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.validate(); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	ctx := r.Context()
	mgr, _ := auth.ManagerFrom(ctx)
	scope := auth.ScopeFilter(ctx)
	owner := in.OwnerManagerID
	if scope != nil {
		owner = scope.ManagerID
	}

	usernames := in.usernames()
	collisions, err := m.findCollisions(ctx, usernames)
	if err != nil {
		m.internalError(w, "batch collision check", err)
		return
	}

	// Licence cap (FR-124/FR-105): refused up front for the WHOLE batch,
	// before a single row is written — never partway through (binding rule,
	// gate item 5). Only the rows that would actually land count toward the
	// cap check; a dry-run against an already-colliding username set still
	// reports the true would-be count via the cap message so an operator sees
	// the wall before retrying with a real profile/prefix.
	wouldCreate := len(usernames) - len(collisions)
	capStatus, err := m.subscriberCapStatus(ctx, wouldCreate)
	if err != nil {
		m.internalError(w, "batch license cap check", err)
		return
	}

	if in.DryRun {
		resp := batchDryRunResponse{Usernames: usernames, Collisions: collisions, Total: len(usernames)}
		if capStatus.Blocked {
			resp.CapBlocked = true
			resp.CapMessage = fmt.Sprintf(
				"this batch would push the subscriber count past this license's limit of %d (plus a %d buffer)",
				capStatus.Cap, capStatus.Buffer)
		}
		httpapi.JSON(w, http.StatusOK, resp)
		return
	}

	if capStatus.Blocked {
		httpapi.Error(w, http.StatusForbidden, "license_subscriber_cap",
			fmt.Sprintf("this batch would push the subscriber count past this license's limit of %d (plus a %d buffer); reduce the count or renew/upgrade",
				capStatus.Cap, capStatus.Buffer))
		return
	}

	job := &bulkJob{
		ID: newJobID(), Action: "create_batch", Status: "running",
		Total: len(usernames), Failures: []bulkFailure{}, StartedAt: time.Now(),
	}
	m.jobs.add(job)

	actorID, ip, ua := mgr.ID, mgr.IP, mgr.UA
	actorScoped := scope != nil
	go m.runBatch(job, in, usernames, collisions, owner, actorID, actorScoped, ip, ua)

	httpapi.JSON(w, http.StatusAccepted, job.snapshot())
}

// findCollisions returns the subset of usernames that already exist —
// case-insensitively, matching FR-1.1's uniqueness rule.
func (m *Module) findCollisions(ctx context.Context, usernames []string) ([]string, error) {
	rows, err := m.db.Query(ctx,
		`SELECT username::text FROM subscribers WHERE subscriber_fold(username::text) = ANY(
		   SELECT subscriber_fold(u) FROM unnest($1::text[]) AS u)`, usernames)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		existing[strings.ToLower(u)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var collisions []string
	for _, u := range usernames {
		if existing[strings.ToLower(u)] {
			collisions = append(collisions, u)
		}
	}
	return collisions, nil
}

func (m *Module) runBatch(job *bulkJob, in batchInput, usernames, collisions []string, owner, actorID string, actorScoped bool, ip, ua string) {
	isCollision := make(map[string]bool, len(collisions))
	for _, c := range collisions {
		isCollision[c] = true
	}

	var created []string
	for _, username := range usernames {
		if isCollision[username] {
			job.mu.Lock()
			job.Done++
			job.Failed++
			job.Failures = append(job.Failures, bulkFailure{Username: username, Error: "username already exists"})
			job.mu.Unlock()
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		id, password, err := m.createBatchRow(ctx, in, username, owner)
		cancel()

		job.mu.Lock()
		job.Done++
		if err != nil {
			job.Failed++
			job.Failures = append(job.Failures, bulkFailure{Username: username, Error: err.Error()})
			job.mu.Unlock()
			continue
		}
		job.Succeeded++
		if password != "" {
			if job.Passwords == nil {
				job.Passwords = map[string]string{}
			}
			job.Passwords[username] = password
		}
		job.mu.Unlock()

		actx, acancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = auth.AuditActor(actx, actorID, ip, ua, "subscriber.batch_create", "subscriber", id, nil,
			map[string]any{"username": username, "batch_id": job.ID, "password_generated": password != ""})
		acancel()
		created = append(created, id)

		if in.Invoicing == invoiceEach {
			if inv := currentInvoicer(); inv != nil {
				ictx, icancel := context.WithTimeout(context.Background(), 15*time.Second)
				if ierr := inv.InvoiceSubscriber(ictx, id, actorID, actorScoped); ierr != nil {
					job.mu.Lock()
					job.Failures = append(job.Failures, bulkFailure{SubscriberID: id, Username: username,
						Error: "created but invoicing failed: " + ierr.Error()})
					job.mu.Unlock()
				}
				icancel()
			}
		}
	}

	if in.Invoicing == invoiceBatch && len(created) > 0 {
		if inv := currentInvoicer(); inv != nil {
			ictx, icancel := context.WithTimeout(context.Background(), 30*time.Second)
			if ierr := inv.InvoiceBatch(ictx, in.ProfileID, actorID, actorScoped, len(created), job.ID); ierr != nil {
				job.mu.Lock()
				job.Failures = append(job.Failures, bulkFailure{Error: "batch invoice failed: " + ierr.Error()})
				job.mu.Unlock()
			}
			icancel()
		}
	}

	job.mu.Lock()
	job.Status = "completed"
	job.mu.Unlock()
}

// createBatchRow provisions one subscriber row. Returns the generated
// password when password_mode is "generated" (empty for "shared" — nothing
// new to reveal there).
func (m *Module) createBatchRow(ctx context.Context, in batchInput, username, owner string) (id string, generatedPassword string, err error) {
	plainPassword := in.SharedPassword
	if in.PasswordMode == "generated" {
		generatedPassword, err = randomPassword(12)
		if err != nil {
			return "", "", err
		}
		plainPassword = generatedPassword
	}
	enc, err := crypto.Encrypt([]byte(plainPassword))
	if err != nil {
		return "", "", err
	}

	// Provisioning status/expiry depends on the invoicing choice:
	//   - "none"/"batch": expiry is set directly (from the request, else the
	//     profile's own duration from now) and the account is active
	//     immediately — no money moved for "none"; "batch" moves it in one
	//     lump sum after every row lands (runBatch).
	//   - "each": created disabled with a clear reason; InvoiceSubscriber
	//     (the real FR-19 renewal) is what flips it active and sets the real
	//     expiry — a subscriber that was never successfully invoiced must
	//     never look indistinguishable from one that was.
	status := "active"
	disabledReason := ""
	var expiresAt *time.Time
	if in.Invoicing == invoiceEach {
		status = "disabled"
		disabledReason = "pending batch invoice"
	} else {
		expiresAt, err = m.resolveBatchExpiry(ctx, in)
		if err != nil {
			return "", "", err
		}
	}

	var disabledReasonPtr *string
	if disabledReason != "" {
		disabledReasonPtr = &disabledReason
	}

	err = m.db.QueryRow(ctx,
		`INSERT INTO subscribers
		   (username, password_enc, status, profile_id, owner_manager_id, expires_at,
		    disabled_reason, service_type, has_password, auto_renew, quota_cycle_anchor)
		 VALUES ($1,$2,$3,$4::uuid,NULLIF($5,'')::uuid,$6,$7,$8,true,$9, now())
		 RETURNING id::text`,
		username, enc, status, in.ProfileID, owner, expiresAt, disabledReasonPtr,
		in.ServiceType, in.AutoRenew).Scan(&id)
	if err != nil {
		return "", "", err
	}
	_ = radius.InvalidatePolicy(id)
	return id, generatedPassword, nil
}

// resolveBatchExpiry: the request's explicit expires_at, else now + the
// profile's duration_days — the same "a provisioned account is valid for one
// billing cycle" default a renewal would produce, without actually charging
// anything (invoicing "none"/"batch" only).
func (m *Module) resolveBatchExpiry(ctx context.Context, in batchInput) (*time.Time, error) {
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("invalid expires_at: %w", err)
		}
		return &t, nil
	}
	var durationDays int
	if err := m.db.QueryRow(ctx, `SELECT duration_days FROM profiles WHERE id = $1::uuid`, in.ProfileID).
		Scan(&durationDays); err != nil {
		return nil, err
	}
	t := time.Now().UTC().AddDate(0, 0, durationDays)
	return &t, nil
}

const passwordAlphabet = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ"

// randomPassword generates a crypto-random password over an alphabet with no
// visually ambiguous characters (0/O, 1/l/I) — an operator hand-copying a
// batch-generated password onto a printed slip has the same misread problem
// vouchers already solved for (billing/voucher.go's voucherAlphabet).
func randomPassword(n int) (string, error) {
	max := big.NewInt(int64(len(passwordAlphabet)))
	var b strings.Builder
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(passwordAlphabet[idx.Int64()])
	}
	return b.String(), nil
}
