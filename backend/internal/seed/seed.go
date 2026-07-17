// Package seed loads dev/demo fixtures (invoked by `hikrad-api seed`, wrapped by
// the repo-root `make seed`). Phase 2 (Agent 4) expands it from the Phase-1
// smoke fixtures to a realistic demo base for gate testing and screenshots:
//
//   - manager admin/admin (raw SQL into Agent 1's managers table — no import,
//     per the cross-agent seeding rule established in Phase 1)
//   - one NAS at 10.0.0.1 (raw SQL into Agent 2's nas table) so a seeded
//     subscriber authenticates through the real policy engine end to end
//   - three service profiles (Basic 10M / Home 25M / Business 50M)
//   - subscriber testuser/testpass on Basic 10M (kept for existing tests)
//   - 200 demo subscribers with Arabic names + Iraqi phones across the profiles
//
// Subscriber RADIUS passwords and the NAS secret are sealed with the platform
// crypto service (C3) — this replaces the Phase-1 temporary AES helper that used
// to live in this package (removed in Phase 2 per contract C3).
//
// Seeding is idempotent: reruns update the fixtures in place.
package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Run loads the full fixture set — the lean base plus the 200-subscriber demo
// base for screenshots and manual gate walkthroughs. This is what `make seed`
// (via `hikrad-api seed`) loads. Migrations must already be applied;
// encryptionKey is platform.Config.EncryptionKey (32 bytes), used to configure
// the process crypto service so seed seals secrets exactly as the server would.
func Run(ctx context.Context, db *pgxpool.Pool, encryptionKey []byte) error {
	base, err := runBase(ctx, db, encryptionKey)
	if err != nil {
		return err
	}
	if err := seedDemoSubscribers(ctx, db, base.profiles, base.adminID); err != nil {
		return fmt.Errorf("seed demo subscribers: %w", err)
	}
	return nil
}

// RunBase loads only the minimal fixtures every environment needs (admin, one
// NAS, the three plans, and the testuser subscriber) — no bulk demo data. The
// integration/gate tests use this so their whole-table assertions aren't skewed
// by 200 demo rows; `make seed` uses the fuller Run above.
func RunBase(ctx context.Context, db *pgxpool.Pool, encryptionKey []byte) error {
	_, err := runBase(ctx, db, encryptionKey)
	return err
}

type baseResult struct {
	adminID  string
	profiles map[string]string
}

func runBase(ctx context.Context, db *pgxpool.Pool, encryptionKey []byte) (baseResult, error) {
	if err := crypto.Configure(encryptionKey); err != nil {
		return baseResult{}, fmt.Errorf("configure crypto: %w", err)
	}
	adminID, err := seedManager(ctx, db)
	if err != nil {
		return baseResult{}, fmt.Errorf("seed manager: %w", err)
	}
	if err := seedNAS(ctx, db); err != nil {
		return baseResult{}, fmt.Errorf("seed nas: %w", err)
	}
	profiles, err := seedProfiles(ctx, db)
	if err != nil {
		return baseResult{}, fmt.Errorf("seed profiles: %w", err)
	}
	if err := seedSubscriber(ctx, db, profiles["Basic 10M"]); err != nil {
		return baseResult{}, fmt.Errorf("seed subscriber: %w", err)
	}
	return baseResult{adminID: adminID, profiles: profiles}, nil
}

func seedManager(ctx context.Context, db *pgxpool.Pool) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	var id string
	err = db.QueryRow(ctx,
		`INSERT INTO managers (username, password_hash, role)
		 VALUES ('admin', $1, 'admin')
		 ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash, role = EXCLUDED.role
		 RETURNING id::text`,
		string(hash)).Scan(&id)
	return id, err
}

// seedNAS registers the demo NAS at 10.0.0.1 so testuser (and the harness)
// authenticate through the known-NAS check (FR-13.2). Raw SQL into Agent 2's
// table, sealed secret; idempotent on the unique ip.
//
// The NAS carries both a PPPoE and a Hotspot service instance (FR-62): the
// demo set seeds hotspot-only and dual subscribers, and a hotspot login on a
// NAS with no enabled hotspot instance rejects nas_not_allowed (C6 step 2), so
// a PPPoE-only demo NAS would make every seeded hotspot account un-loginable.
func seedNAS(ctx context.Context, db *pgxpool.Pool) error {
	secret, err := crypto.Encrypt([]byte("testing123"))
	if err != nil {
		return err
	}
	var nasID string
	if err := db.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc, vendor, enabled)
		 VALUES ('Demo NAS', '10.0.0.1'::inet, $1, 'mikrotik', true)
		 ON CONFLICT (ip) DO UPDATE SET secret_enc = EXCLUDED.secret_enc, enabled = true
		 RETURNING id::text`,
		secret).Scan(&nasID); err != nil {
		return err
	}
	for _, s := range []struct{ service, label, server string }{
		{"pppoe", "Demo PPPoE", "hikrad-pppoe"},
		{"hotspot", "Demo Hotspot", "hotspot1"},
	} {
		if _, err := db.Exec(ctx,
			`INSERT INTO nas_services (nas_id, service, label, ros_server_name, enabled)
			 SELECT $1::uuid, $2, $3, $4, true
			  WHERE NOT EXISTS (
			        SELECT 1 FROM nas_services WHERE nas_id = $1::uuid AND service = $2 AND ros_server_name = $4)`,
			nasID, s.service, s.label, s.server); err != nil {
			return err
		}
	}
	return nil
}

// profileSpec is one seeded plan. Speeds in kbps (1024-multiples render as "NM").
type profileSpec struct {
	name         string
	priceIQD     int64
	durationDays int
	downKbps     int
	upKbps       int
}

var demoProfiles = []profileSpec{
	{"Basic 10M", 25000, 30, 10240, 10240},
	{"Home 25M", 40000, 30, 25600, 25600},
	{"Business 50M", 75000, 30, 51200, 51200},
}

// seedProfiles upserts the demo plans and returns name→id.
func seedProfiles(ctx context.Context, db *pgxpool.Pool) (map[string]string, error) {
	out := make(map[string]string, len(demoProfiles))
	for _, p := range demoProfiles {
		var id string
		err := db.QueryRow(ctx, `SELECT id::text FROM profiles WHERE name = $1`, p.name).Scan(&id)
		if err == nil {
			if _, err := db.Exec(ctx,
				`UPDATE profiles SET price=$2, duration_days=$3, rate_down_kbps=$4, rate_up_kbps=$5
				  WHERE id = $1::uuid`, id, p.priceIQD, p.durationDays, p.downKbps, p.upKbps); err != nil {
				return nil, err
			}
			out[p.name] = id
			continue
		}
		if err := db.QueryRow(ctx,
			`INSERT INTO profiles (name, price, duration_days, rate_down_kbps, rate_up_kbps)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id::text`,
			p.name, p.priceIQD, p.durationDays, p.downKbps, p.upKbps).Scan(&id); err != nil {
			return nil, err
		}
		out[p.name] = id
	}
	return out, nil
}

func seedSubscriber(ctx context.Context, db *pgxpool.Pool, profileID string) error {
	enc, err := crypto.Encrypt([]byte("testpass"))
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
