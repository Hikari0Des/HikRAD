// Package seed loads the Phase-1 dev fixtures (invoked by `hikrad-api seed`,
// which the repo-root `make seed` target wraps):
//
//   - manager admin/admin — written with raw SQL into Agent 1's managers
//     table (no package import, per the task's ownership rule)
//   - subscriber testuser/testpass — password stored AES-GCM-encrypted with
//     HIKRAD_ENCRYPTION_KEY (see crypto.go for why and for how temporary
//     that helper's location is)
//   - profile "Basic 10M" — 10 Mbps down/up, 30 days, 25 000 IQD
//
// Seeding is idempotent: reruns update the fixtures in place.
package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Run loads all dev fixtures. The migrations must already be applied;
// encryptionKey is platform.Config.EncryptionKey (32 bytes).
func Run(ctx context.Context, db *pgxpool.Pool, encryptionKey []byte) error {
	if err := seedManager(ctx, db); err != nil {
		return fmt.Errorf("seed manager: %w", err)
	}
	profileID, err := seedProfile(ctx, db)
	if err != nil {
		return fmt.Errorf("seed profile: %w", err)
	}
	if err := seedSubscriber(ctx, db, encryptionKey, profileID); err != nil {
		return fmt.Errorf("seed subscriber: %w", err)
	}
	return nil
}

func seedManager(ctx context.Context, db *pgxpool.Pool) error {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx,
		`INSERT INTO managers (username, password_hash, role)
		 VALUES ('admin', $1, 'admin')
		 ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash, role = EXCLUDED.role`,
		string(hash))
	return err
}

func seedProfile(ctx context.Context, db *pgxpool.Pool) (string, error) {
	// profiles.name has no unique constraint in the frozen C6 schema, so
	// upsert manually to stay idempotent.
	var id string
	err := db.QueryRow(ctx, `SELECT id::text FROM profiles WHERE name = 'Basic 10M'`).Scan(&id)
	if err == nil {
		_, err = db.Exec(ctx,
			`UPDATE profiles SET price_iqd = 25000, duration_days = 30,
			        rate_down_kbps = 10240, rate_up_kbps = 10240
			  WHERE id = $1::uuid`, id)
		return id, err
	}
	err = db.QueryRow(ctx,
		`INSERT INTO profiles (name, price_iqd, duration_days, rate_down_kbps, rate_up_kbps)
		 VALUES ('Basic 10M', 25000, 30, 10240, 10240)
		 RETURNING id::text`).Scan(&id)
	return id, err
}

func seedSubscriber(ctx context.Context, db *pgxpool.Pool, encryptionKey []byte, profileID string) error {
	enc, err := EncryptPassword("testpass", encryptionKey)
	if err != nil {
		return err
	}
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	_, err = db.Exec(ctx,
		`INSERT INTO subscribers (username, password_enc, name, status, profile_id, expires_at)
		 VALUES ('testuser', $1, 'Test User', 'active', $2::uuid, $3)
		 ON CONFLICT (username) DO UPDATE
		   SET password_enc = EXCLUDED.password_enc,
		       status = EXCLUDED.status,
		       profile_id = EXCLUDED.profile_id,
		       expires_at = EXCLUDED.expires_at`,
		enc, profileID, expires)
	return err
}
