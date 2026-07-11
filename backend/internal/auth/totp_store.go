package auth

// TOTP + backup-code persistence and second-factor verification (FR-28.1).
// The active/pending secrets live on the managers row (AES-GCM sealed); backup
// codes live one-per-row in manager_backup_codes, stored only as sha256 and
// consumed atomically (single-use, reuse rejected).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// settingRequire2FA is the global "require 2FA for all managers" flag (bool).
const settingRequire2FA = "security.require_2fa"

const backupCodeCount = 10

// backupCodeAlphabet excludes ambiguous characters so a code is easy to read
// off a printed sheet.
const backupCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

var errNoPendingEnrolment = errors.New("auth: no pending totp enrolment")

// hashBackupCode is the stored form of a backup code (uppercased, dashes
// stripped, sha256). Codes are high-entropy so a fast hash is sufficient — same
// discipline as refresh secrets.
func hashBackupCode(code string) []byte {
	norm := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte(norm))
	return sum[:]
}

// generateBackupCodes returns backupCodeCount fresh codes formatted "XXXXX-XXXXX".
func generateBackupCodes() ([]string, error) {
	out := make([]string, 0, backupCodeCount)
	for i := 0; i < backupCodeCount; i++ {
		buf := make([]byte, 10)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		var b strings.Builder
		for j, x := range buf {
			if j == 5 {
				b.WriteByte('-')
			}
			b.WriteByte(backupCodeAlphabet[int(x)%len(backupCodeAlphabet)])
		}
		out = append(out, b.String())
	}
	return out, nil
}

// storePendingSecret seals and stores a not-yet-verified TOTP secret.
func storePendingSecret(ctx context.Context, db *pgxpool.Pool, managerID, secret string) error {
	enc, err := svc.crypto.Encrypt([]byte(secret))
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx,
		`UPDATE managers SET totp_pending_secret_enc = $2 WHERE id = $1::uuid`, managerID, enc)
	return err
}

// activatePendingSecret promotes the pending secret to active, enables TOTP,
// replaces backup codes with fresh hashes, and revokes other sessions — all in
// one transaction. Returns errNoPendingEnrolment if there is nothing pending.
func activatePendingSecret(ctx context.Context, db *pgxpool.Pool, managerID string, backupHashes [][]byte, keepSessionID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var pending []byte
	err = tx.QueryRow(ctx,
		`SELECT totp_pending_secret_enc FROM managers WHERE id = $1::uuid FOR UPDATE`, managerID).Scan(&pending)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return errNoPendingEnrolment
	}
	if _, err := tx.Exec(ctx,
		`UPDATE managers
		    SET totp_secret_enc = totp_pending_secret_enc,
		        totp_pending_secret_enc = NULL,
		        totp_enabled = true,
		        totp_enrolled_at = now()
		  WHERE id = $1::uuid`, managerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM manager_backup_codes WHERE manager_id = $1::uuid`, managerID); err != nil {
		return err
	}
	for _, h := range backupHashes {
		if _, err := tx.Exec(ctx,
			`INSERT INTO manager_backup_codes (manager_id, code_hash) VALUES ($1::uuid, $2)`, managerID, h); err != nil {
			return err
		}
	}
	// Enabling 2FA invalidates other live sessions (FR-29 discipline).
	if keepSessionID == "" {
		if _, err := tx.Exec(ctx, `UPDATE panel_sessions SET revoked = true WHERE manager_id = $1::uuid AND revoked = false`, managerID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE panel_sessions SET revoked = true WHERE manager_id = $1::uuid AND revoked = false AND id <> $2::uuid`, managerID, keepSessionID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// disableTOTP clears all TOTP state and backup codes for a manager. Used by
// self-service disable and admin reset.
func disableTOTP(ctx context.Context, db *pgxpool.Pool, managerID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx,
		`UPDATE managers
		    SET totp_enabled = false, totp_secret_enc = NULL,
		        totp_pending_secret_enc = NULL, totp_enrolled_at = NULL
		  WHERE id = $1::uuid`, managerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM manager_backup_codes WHERE manager_id = $1::uuid`, managerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// consumeBackupCode atomically marks the single unused backup code matching
// code as used, returning true if one was consumed (single-use; reuse fails).
func consumeBackupCode(ctx context.Context, db *pgxpool.Pool, managerID, code string) (bool, error) {
	hash := hashBackupCode(code)
	tag, err := db.Exec(ctx,
		`UPDATE manager_backup_codes SET used_at = now()
		  WHERE ctid IN (
		      SELECT ctid FROM manager_backup_codes
		       WHERE manager_id = $1::uuid AND code_hash = $2 AND used_at IS NULL
		       LIMIT 1
		  )`, managerID, hash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// verifyManagerSecondFactor checks a login's second factor: first the TOTP code
// against the active secret, then (on miss) a one-time backup code.
func verifyManagerSecondFactor(ctx context.Context, m *managerAuthRow, code string) (bool, error) {
	if len(m.TOTPSecretEnc) > 0 {
		secret, err := svc.crypto.Decrypt(m.TOTPSecretEnc)
		if err != nil {
			return false, err
		}
		if verifyTOTP(string(secret), code, time.Now()) {
			return true, nil
		}
	}
	return consumeBackupCode(ctx, svc.db, m.ID, code)
}

// decodeSecretForCheck decrypts a manager's active secret (helper for disable).
func decodeSecretForCheck(enc []byte) (string, error) {
	if len(enc) == 0 {
		return "", errNoPendingEnrolment
	}
	pt, err := svc.crypto.Decrypt(enc)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// loadTOTPSecrets returns a manager's active + pending sealed secrets.
func loadTOTPSecrets(ctx context.Context, db *pgxpool.Pool, managerID string) (active, pending []byte, err error) {
	err = db.QueryRow(ctx,
		`SELECT totp_secret_enc, totp_pending_secret_enc FROM managers WHERE id = $1::uuid`, managerID).
		Scan(&active, &pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	return active, pending, err
}
