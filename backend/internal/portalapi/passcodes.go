package portalapi

// Shared passcode machinery (FR-169 self-service guest registration + FR-172
// OTP login, migration 0632). Both features are "prove you own this phone
// number, get in" — the phase brief's own instruction is that OTP login
// reuses FR-169's machinery rather than a second implementation, so every
// abuse control here (one live code per number, cooldown after expiry,
// send-time validity, per-MAC attempt recording, daily-volume + entitlement
// cap) applies identically regardless of which feature called it.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/hikrad/hikrad/internal/notify"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	passcodePurposeRegister = "register"
	passcodePurposeOTPLogin = "otp_login"

	attemptSend             = "send"
	attemptVerifyOK         = "verify_ok"
	attemptVerifyFail       = "verify_fail"
	attemptCooldownRefused  = "cooldown_refused"
	attemptDuplicateRefused = "duplicate_refused"

	maxVerifyAttempts = 5 // a code burned after this many wrong guesses, forcing a fresh request
)

var (
	errPasscodeFeatureDisabled = errors.New("portalapi: guest registration/OTP is disabled")
	errPasscodeNotLicensed     = errors.New("portalapi: self_service entitlement not licensed")
	errPasscodeNotConfigured   = errors.New("portalapi: no messaging channel configured for passcodes")
	errPasscodeDailyCapReached = errors.New("portalapi: daily passcode volume cap reached")
	errPasscodeAlreadyLive     = errors.New("portalapi: check your messages") // "one live passcode" re-ask
	errPasscodeCooldown        = errors.New("portalapi: try again after the cooldown")
	errPasscodeInvalid         = errors.New("portalapi: invalid or expired code")
)

type guestRegistrationConfig struct {
	Enabled           bool
	DailyCap          int
	PasscodeTTLMin    int
	CooldownMin       int
	DefaultProfileID  string
}

func loadGuestRegistrationConfig(ctx context.Context, s platform.Settings) guestRegistrationConfig {
	cfg := guestRegistrationConfig{DailyCap: 100, PasscodeTTLMin: 10, CooldownMin: 5}
	if v, err := platform.Get[bool](ctx, s, "guest_registration.enabled"); err == nil {
		cfg.Enabled = v
	}
	if v, err := platform.Get[int](ctx, s, "guest_registration.daily_cap"); err == nil && v > 0 {
		cfg.DailyCap = v
	}
	if v, err := platform.Get[int](ctx, s, "guest_registration.passcode_ttl_minutes"); err == nil && v > 0 {
		cfg.PasscodeTTLMin = v
	}
	if v, err := platform.Get[int](ctx, s, "guest_registration.cooldown_minutes"); err == nil && v >= 0 {
		cfg.CooldownMin = v
	}
	if v, err := platform.Get[string](ctx, s, "guest_registration.default_profile_id"); err == nil {
		cfg.DefaultProfileID = v
	}
	return cfg
}

// generateCode returns a cryptographically random 6-digit numeric code.
func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func hashCode(code string) []byte {
	sum := sha256.Sum256([]byte(code))
	return sum[:]
}

func recordPasscodeAttempt(ctx context.Context, db *pgxpool.Pool, purpose, phone, mac, action string) {
	_, _ = db.Exec(ctx,
		`INSERT INTO guest_passcode_attempts (purpose, phone, mac, action) VALUES ($1,$2,$3,$4)`,
		purpose, phone, mac, action)
}

// issuePasscode runs every FR-169 abuse control and, if all pass, queues one
// message through notify.EnqueuePasscode. subscriberID is "" for a
// registration passcode, set for an OTP login passcode. name is held on the
// row (register only) until the account is actually created at verify time.
func issuePasscode(ctx context.Context, m *Module, purpose, phone, subscriberID, mac, name, locale string) error {
	cfg := loadGuestRegistrationConfig(ctx, m.settings)
	if !cfg.Enabled {
		return errPasscodeFeatureDisabled
	}
	if !platform.LicenseHasEntitlement(license.EntitlementSelfService) {
		return errPasscodeNotLicensed
	}

	// Daily volume cap (FR-169 abuse control), counted from send attempts
	// across BOTH purposes — a single licence entitlement caps the whole
	// credential-free-passcode surface, not each feature separately.
	var sentToday int
	_ = m.db.QueryRow(ctx,
		`SELECT count(*) FROM guest_passcode_attempts WHERE action = $1 AND at >= now() - interval '24 hours'`,
		attemptSend).Scan(&sentToday)
	if sentToday >= cfg.DailyCap {
		return errPasscodeDailyCapReached
	}

	// "One live passcode per mobile number": if an unconsumed row already
	// exists for (purpose, phone) AND it has not yet expired, this is a
	// re-ask — say "check your messages" and send nothing new (never a
	// second code). An unconsumed row that HAS expired is not "live" by any
	// reasonable reading of that word — it falls through to the cooldown
	// check below instead, which is the control that actually governs a
	// re-request after expiry (found live: an earlier version of this
	// check ignored expiry entirely, so an expired-but-unconsumed row
	// silently blocked every future request forever with "check your
	// messages" and the cooldown branch was unreachable dead code).
	existing, err := loadLivePasscode(ctx, m.db, purpose, phone)
	if err == nil && !time.Now().After(existing.ExpiresAt) {
		recordPasscodeAttempt(ctx, m.db, purpose, phone, mac, attemptDuplicateRefused)
		return errPasscodeAlreadyLive
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// Cooldown after expiry: the most recent row for this (purpose, phone)
	// expired (unconsumed) within the last cooldown_minutes.
	var lastExpiredAt *time.Time
	qerr := m.db.QueryRow(ctx,
		`SELECT expires_at FROM guest_passcodes
		  WHERE purpose = $1 AND phone = $2 AND consumed_at IS NULL AND expires_at < now()
		  ORDER BY expires_at DESC LIMIT 1`,
		purpose, phone).Scan(&lastExpiredAt)
	if qerr != nil && !errors.Is(qerr, pgx.ErrNoRows) {
		return qerr
	}
	if lastExpiredAt != nil && time.Since(*lastExpiredAt) < time.Duration(cfg.CooldownMin)*time.Minute {
		recordPasscodeAttempt(ctx, m.db, purpose, phone, mac, attemptCooldownRefused)
		return errPasscodeCooldown
	}

	code, err := generateCode()
	if err != nil {
		return err
	}
	ttl := time.Duration(cfg.PasscodeTTLMin) * time.Minute
	now := time.Now()
	var subID *string
	if subscriberID != "" {
		subID = &subscriberID
	}
	// An expired-but-never-consumed row for this (purpose, phone) — past its
	// cooldown, or there would never have reached here — must be closed out
	// before inserting the new one: guest_passcodes_live_idx is a UNIQUE
	// (purpose, phone) WHERE consumed_at IS NULL partial index, so a second
	// unconsumed row for the same identity would violate it. This is not a
	// real "consumption" (the visitor never entered a code), just marking
	// the superseded row no-longer-live; verifyPasscode can never match it
	// again either way since a fresh row now owns the live slot.
	if _, err := m.db.Exec(ctx,
		`UPDATE guest_passcodes SET consumed_at = now()
		  WHERE purpose = $1 AND phone = $2 AND consumed_at IS NULL AND expires_at < now()`,
		purpose, phone); err != nil {
		return err
	}
	if _, err := m.db.Exec(ctx,
		`INSERT INTO guest_passcodes (purpose, phone, subscriber_id, code_hash, mac, name, sent_at, expires_at)
		 VALUES ($1, $2, $3::uuid, $4, $5, $6, $7, $8)`,
		purpose, phone, subID, hashCode(code), mac, name, now, now.Add(ttl)); err != nil {
		return err
	}

	res, err := notify.EnqueuePasscode(ctx, m.db, notify.PasscodeInput{
		EventKey: passcodeEventKey(purpose), Phone: phone, SubscriberID: subscriberID,
		Locale: locale, Code: code, TTLMinutes: cfg.PasscodeTTLMin,
	})
	if err != nil {
		return err
	}
	recordPasscodeAttempt(ctx, m.db, purpose, phone, mac, attemptSend)
	if !res.Queued {
		return errPasscodeNotConfigured
	}
	return nil
}

func passcodeEventKey(purpose string) string {
	if purpose == passcodePurposeOTPLogin {
		return notify.EventOTPLoginPasscode
	}
	return notify.EventGuestRegistrationPasscode
}

type livePasscode struct {
	ID           string
	CodeHash     []byte
	Name         string
	ExpiresAt    time.Time
	AttemptCount int
}

func loadLivePasscode(ctx context.Context, db *pgxpool.Pool, purpose, phone string) (livePasscode, error) {
	var p livePasscode
	err := db.QueryRow(ctx,
		`SELECT id::text, code_hash, name, expires_at, attempt_count
		   FROM guest_passcodes WHERE purpose = $1 AND phone = $2 AND consumed_at IS NULL`,
		purpose, phone).Scan(&p.ID, &p.CodeHash, &p.Name, &p.ExpiresAt, &p.AttemptCount)
	return p, err
}

// verifyPasscode checks code against the live (purpose, phone) row.
// Constant-time comparison; a wrong guess increments attempt_count and,
// past maxVerifyAttempts, the code is burned (treated as invalid) so the
// visitor must request a fresh one rather than brute-force a 6-digit space
// against one outstanding row indefinitely.
func verifyPasscode(ctx context.Context, m *Module, purpose, phone, mac, code string) (name string, ok bool, err error) {
	p, err := loadLivePasscode(ctx, m.db, purpose, phone)
	if errors.Is(err, pgx.ErrNoRows) {
		recordPasscodeAttempt(ctx, m.db, purpose, phone, mac, attemptVerifyFail)
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if time.Now().After(p.ExpiresAt) || p.AttemptCount >= maxVerifyAttempts {
		recordPasscodeAttempt(ctx, m.db, purpose, phone, mac, attemptVerifyFail)
		return "", false, nil
	}
	if subtle.ConstantTimeCompare(hashCode(code), p.CodeHash) != 1 {
		_, _ = m.db.Exec(ctx, `UPDATE guest_passcodes SET attempt_count = attempt_count + 1 WHERE id = $1::uuid`, p.ID)
		recordPasscodeAttempt(ctx, m.db, purpose, phone, mac, attemptVerifyFail)
		return "", false, nil
	}
	if _, err := m.db.Exec(ctx, `UPDATE guest_passcodes SET consumed_at = now() WHERE id = $1::uuid`, p.ID); err != nil {
		return "", false, err
	}
	recordPasscodeAttempt(ctx, m.db, purpose, phone, mac, attemptVerifyOK)
	return p.Name, true, nil
}
