package subscribers

// Portal credential verification + self-service fields (Phase 4, contract C2,
// FR-41.1/FR-44). VerifyPassword is the narrow decrypt point the portal login
// path is allowed: it takes username+password in, returns a bool out —
// cleartext never leaves this function (not logged, not returned, not stored).
// It reuses the same password_enc column and A's crypto service B's RADIUS
// authorize path already trusts, so a portal password change and a PPPoE
// credential change are the same write (SetPassword below).

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PortalIdentity is the minimal subscriber identity the portal needs once a
// credential has verified.
type PortalIdentity struct {
	ID       string
	Username string
	Name     string
	Language string
}

// VerifyPassword checks username/password against the sealed RADIUS
// credential (NFR-4.2). A missing user, a decrypt failure (wrong key/corrupt
// data) and a wrong password are all indistinguishable to the caller — only a
// bool is returned, never the plaintext or a reason.
func VerifyPassword(ctx context.Context, db *pgxpool.Pool, username, password string) (PortalIdentity, bool, error) {
	var (
		id          PortalIdentity
		name        *string
		language    *string
		passwordEnc []byte
	)
	err := db.QueryRow(ctx,
		`SELECT id::text, username, name, language, password_enc FROM subscribers WHERE username = $1`,
		username).Scan(&id.ID, &id.Username, &name, &language, &passwordEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return PortalIdentity{}, false, nil
	}
	if err != nil {
		return PortalIdentity{}, false, err
	}
	id.Name = strOr(name, "")
	id.Language = strOr(language, "en")
	if len(passwordEnc) == 0 {
		return PortalIdentity{}, false, nil
	}
	plain, derr := crypto.Decrypt(passwordEnc)
	if derr != nil {
		return PortalIdentity{}, false, nil
	}
	// Passwordless hotspot accounts (item 13) seal an empty credential; letting
	// "" match "" here would open the portal to anyone knowing a username.
	if len(plain) == 0 {
		return PortalIdentity{}, false, nil
	}
	match := subtle.ConstantTimeCompare(plain, []byte(password)) == 1
	for i := range plain {
		plain[i] = 0
	}
	if !match {
		return PortalIdentity{}, false, nil
	}
	return id, true, nil
}

// LoadIdentity loads a PortalIdentity by subscriber id (used on token
// refresh, where only the id — not the password — is available).
func LoadIdentity(ctx context.Context, db *pgxpool.Pool, subscriberID string) (PortalIdentity, error) {
	var id PortalIdentity
	var name, language *string
	err := db.QueryRow(ctx, `SELECT id::text, username, name, language FROM subscribers WHERE id = $1::uuid`, subscriberID).
		Scan(&id.ID, &id.Username, &name, &language)
	if err != nil {
		return PortalIdentity{}, err
	}
	id.Name = strOr(name, "")
	id.Language = strOr(language, "en")
	return id, nil
}

// SetPassword re-encrypts a subscriber's RADIUS credential (FR-44) and
// invalidates B's cached policy so the PPPoE side takes the new password
// immediately — the UI must warn the subscriber their PPPoE login changes too.
func SetPassword(ctx context.Context, db *pgxpool.Pool, subscriberID, newPassword string) error {
	enc, err := crypto.Encrypt([]byte(newPassword))
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `UPDATE subscribers SET password_enc = $2, updated_at = now() WHERE id = $1::uuid`,
		subscriberID, enc); err != nil {
		return err
	}
	return radius.InvalidatePolicy(subscriberID)
}

// SetPhone updates the one subscriber-safe contact field FR-44 exposes to
// self-service (never profile/expiry/MAC/status).
func SetPhone(ctx context.Context, db *pgxpool.Pool, subscriberID, phone string) error {
	_, err := db.Exec(ctx, `UPDATE subscribers SET phone = NULLIF($2,''), updated_at = now() WHERE id = $1::uuid`,
		subscriberID, phone)
	return err
}

// SetName lets a subscriber correct their own display name (FR-44/item 22 —
// subscriber-safe field, same class as SetPhone).
func SetName(ctx context.Context, db *pgxpool.Pool, subscriberID, name string) error {
	_, err := db.Exec(ctx, `UPDATE subscribers SET name = NULLIF($2,''), updated_at = now() WHERE id = $1::uuid`,
		subscriberID, name)
	return err
}

// SetEmail lets a subscriber set their own email (FR-85/FR-44 — subscriber-safe
// field, same pattern as SetPhone).
func SetEmail(ctx context.Context, db *pgxpool.Pool, subscriberID, email string) error {
	_, err := db.Exec(ctx, `UPDATE subscribers SET email = NULLIF($2,''), updated_at = now() WHERE id = $1::uuid`,
		subscriberID, email)
	return err
}

// SetLanguage persists the subscriber's portal language preference (FR-43).
func SetLanguage(ctx context.Context, db *pgxpool.Pool, subscriberID, language string) error {
	_, err := db.Exec(ctx, `UPDATE subscribers SET language = $2, updated_at = now() WHERE id = $1::uuid`,
		subscriberID, language)
	return err
}

// PortalProfile is the read-only composition source for GET /portal/me
// (Decision 21: no quota total/remaining field exists anywhere on this type).
type PortalProfile struct {
	SubscriberID     string
	Username         string
	Name             string
	Phone            string
	Status           string
	Language         string
	ExpiresAt        *time.Time
	ProfileName      string
	ProfileDownKbps  int
	ProfileUpKbps    int
	QuotaCycleAnchor *time.Time
	CurrentProfileID string
}

// LoadPortalProfile is the single read backing GET /portal/me + the payload
// context for card-payment submission (current profile snapshot). It deals
// only in consumed-safe fields; quota ceilings never enter this struct.
func LoadPortalProfile(ctx context.Context, db *pgxpool.Pool, subscriberID string) (PortalProfile, error) {
	var (
		p           PortalProfile
		name        *string
		phone       *string
		language    *string
		profileName *string
		downKbps    *int
		upKbps      *int
		profileID   *string
	)
	err := db.QueryRow(ctx,
		`SELECT s.id::text, s.username, s.name, s.phone, s.status, s.language, s.expires_at,
		        p.name, p.rate_down_kbps, p.rate_up_kbps, s.quota_cycle_anchor, s.profile_id::text
		   FROM subscribers s LEFT JOIN profiles p ON p.id = s.profile_id
		  WHERE s.id = $1::uuid`, subscriberID).
		Scan(&p.SubscriberID, &p.Username, &name, &phone, &p.Status, &language, &p.ExpiresAt,
			&profileName, &downKbps, &upKbps, &p.QuotaCycleAnchor, &profileID)
	if err != nil {
		return PortalProfile{}, err
	}
	if p.ExpiresAt != nil {
		u := p.ExpiresAt.UTC()
		p.ExpiresAt = &u
	}
	if p.QuotaCycleAnchor != nil {
		u := p.QuotaCycleAnchor.UTC()
		p.QuotaCycleAnchor = &u
	}
	p.Name = strOr(name, "")
	p.Phone = strOr(phone, "")
	p.Language = strOr(language, "en")
	p.ProfileName = strOr(profileName, "")
	p.ProfileDownKbps = intOr(downKbps, 0)
	p.ProfileUpKbps = intOr(upKbps, 0)
	p.CurrentProfileID = strOr(profileID, "")
	return p, nil
}
