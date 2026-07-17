package billing

// Attachment storage + authenticated retrieval (v2-2, FR-78.2, contract
// C10). Files live on local disk under the data directory (NFR-7: never a
// database blob, never remote object storage) — content_type is validated
// against the file's real content at upload time (net/http's magic-byte
// sniffer, never the client-declared header alone), so a forced
// Content-Disposition at retrieval can never be undermined by a client's
// MIME-type lie.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

const (
	maxAttachmentBytes = 10 << 20 // 10MB per file, per the phase brief's own recommended default
	maxAttachments     = 5
)

var allowedAttachmentTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"application/pdf": true,
}

var (
	attachmentsDirOnce sync.Once
	attachmentsDir     string
)

// paymentAttachmentsDir resolves the local-disk root once per process
// (HIKRAD_PAYMENT_ATTACHMENTS_DIR, defaulting to a path under the repo's
// existing data/ convention — same env-var-with-sane-default pattern
// hikrad-acct's HIKRAD_ACCT_SPILL_DIR already established).
func paymentAttachmentsDir() string {
	attachmentsDirOnce.Do(func() {
		attachmentsDir = os.Getenv("HIKRAD_PAYMENT_ATTACHMENTS_DIR")
		if attachmentsDir == "" {
			attachmentsDir = "data/payment-attachments"
		}
	})
	return attachmentsDir
}

var errAttachmentTypeNotAllowed = errors.New("billing: attachment type not allowed")
var errAttachmentTooLarge = errors.New("billing: attachment too large")
var errTooManyAttachments = errors.New("billing: too many attachments")

// storeAttachments writes every uploaded file to <dir>/<ticket-id>/<uuid> and
// records its metadata. Never inside the ticket's own DB transaction (file
// I/O never happens inside a money transaction, mirroring how CoA restore
// and other post-commit side effects already work in renew()) — a write
// failure here is logged as a ticket event by the caller, never fatal.
func (m *Module) storeAttachments(ctx context.Context, ticketID string, files []uploadedFile) error {
	if len(files) == 0 {
		return nil
	}
	if len(files) > maxAttachments {
		return errTooManyAttachments
	}
	dir := filepath.Join(paymentAttachmentsDir(), ticketID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for _, f := range files {
		if len(f.Data) > maxAttachmentBytes {
			return errAttachmentTooLarge
		}
		sniffed := http.DetectContentType(f.Data)
		if !allowedAttachmentTypes[sniffed] {
			return fmt.Errorf("%w: %s", errAttachmentTypeNotAllowed, sniffed)
		}
		id := randToken()
		storedPath := filepath.Join(dir, id)
		if err := os.WriteFile(storedPath, f.Data, 0o640); err != nil {
			return err
		}
		if _, err := m.db.Exec(ctx,
			`INSERT INTO payment_ticket_attachments (ticket_id, filename, stored_path, content_type, size_bytes)
			 VALUES ($1::uuid, $2, $3, $4, $5)`,
			ticketID, f.Filename, storedPath, sniffed, len(f.Data)); err != nil {
			return err
		}
		_ = m.insertTicketEvent(ctx, ticketID, "attachment_added", "", f.Filename)
	}
	return nil
}

type attachmentMeta struct {
	ID          string
	Filename    string
	StoredPath  string
	ContentType string
	SizeBytes   int64
}

// canSeeTicket reports whether the caller in ctx may view ticketID: an
// unscoped (admin) caller, or a scoped caller whose own subscriber owns it
// (mirrors auth.ScopeFilter's row-level cut, applied here since a
// single-ticket fetch bypasses the list-level WHERE clause C9 relies on).
func (m *Module) canSeeTicket(ctx context.Context, ticketID string) (bool, error) {
	scope := auth.ScopeFilter(ctx)
	if scope == nil {
		return true, nil
	}
	var ownerID *string
	err := m.db.QueryRow(ctx,
		`SELECT s.owner_manager_id::text FROM payment_tickets t
		   JOIN subscribers s ON s.id = t.subscriber_id WHERE t.id = $1::uuid`, ticketID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ownerID != nil && *ownerID == scope.ManagerID, nil
}

// getAttachmentHandler serves GET /payment-tickets/{id}/attachments/{attachmentId}
// (contract C10): permission-checked exactly like the ticket itself, served
// with Content-Disposition: attachment and the SNIFFED (never client-
// declared) content type, so an uploaded file is data, never executed, never
// inline-rendered regardless of what it's named.
func (m *Module) getAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	ticketID := chi.URLParam(r, "id")
	attachmentID := chi.URLParam(r, "attachmentId")
	ok, err := m.canSeeTicket(r.Context(), ticketID)
	if err != nil {
		m.internalError(w, "attachment authz", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this ticket")
		return
	}
	var meta attachmentMeta
	err = m.db.QueryRow(r.Context(),
		`SELECT id::text, filename, stored_path, content_type, size_bytes
		   FROM payment_ticket_attachments WHERE id = $1::uuid AND ticket_id = $2::uuid`,
		attachmentID, ticketID).Scan(&meta.ID, &meta.Filename, &meta.StoredPath, &meta.ContentType, &meta.SizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "attachment not found")
		return
	}
	if err != nil {
		m.internalError(w, "attachment lookup", err)
		return
	}
	data, err := os.ReadFile(meta.StoredPath)
	if err != nil {
		m.internalError(w, "attachment read", err)
		return
	}
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+meta.Filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
