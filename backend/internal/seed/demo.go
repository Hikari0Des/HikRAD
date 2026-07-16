package seed

// Demo subscriber base (task 9): 200 realistic subscribers with Arabic names and
// Iraqi mobile numbers, spread across the three seeded profiles and a mix of
// active / expiring / expired states — enough to exercise search, lists, bulk
// actions, scoping and screenshots. Deterministic and idempotent (fixed
// usernames demo0001..demo0200, ON CONFLICT upsert).

import (
	"context"
	"fmt"
	"time"

	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

const demoCount = 200

var arabicFirstNames = []string{
	"محمد", "علي", "حسين", "أحمد", "عمر", "مصطفى", "يوسف", "إبراهيم", "زيد", "كرار",
	"فاطمة", "زينب", "مريم", "نور", "سارة", "رقية", "هدى", "آية", "دعاء", "بتول",
}

var arabicLastNames = []string{
	"العراقي", "البغدادي", "الموصلي", "البصري", "الكربلائي", "النجفي", "الكاظمي",
	"التكريتي", "الأنباري", "الديواني", "السامرائي", "الحلي", "الكوفي", "الرمادي",
}

// seedDemoSubscribers upserts demoCount subscribers. profiles maps name→id;
// ownerID is the admin manager (owner is set so detail/live owner resolution has
// data — admin is unscoped so it does not restrict visibility).
func seedDemoSubscribers(ctx context.Context, db *pgxpool.Pool, profiles map[string]string, ownerID string) error {
	enc, err := crypto.Encrypt([]byte("demopass"))
	if err != nil {
		return err
	}
	profileIDs := []string{profiles["Basic 10M"], profiles["Home 25M"], profiles["Business 50M"]}
	now := time.Now().UTC()

	batch := make([][]any, 0, demoCount)
	for i := 1; i <= demoCount; i++ {
		username := fmt.Sprintf("demo%04d", i)
		name := arabicFirstNames[i%len(arabicFirstNames)] + " " + arabicLastNames[i%len(arabicLastNames)]
		// Iraqi mobile: +9647[5|7|8|9]XXXXXXXX, deterministic from the index.
		prefix := []string{"75", "77", "78", "79"}[i%4]
		phone := fmt.Sprintf("+9647%s%07d", prefix, i)

		// State mix: ~10% expired, ~15% disabled-ish (kept active but expiring
		// soon), rest healthy. expires_at drives the derived status; the sweep
		// aligns the column.
		var status string
		var expires time.Time
		switch {
		case i%10 == 0:
			status, expires = "expired", now.Add(-time.Duration(i%20+1)*24*time.Hour)
		case i%7 == 0:
			status, expires = "active", now.Add(time.Duration(i%5+1)*24*time.Hour) // expiring soon
		default:
			status, expires = "active", now.Add(time.Duration(20+i%40)*24*time.Hour)
		}

		// Spread all three FR-61 service types across the demo set so the
		// panel's service_type filter has something to show in every position:
		// mostly pppoe, every 5th dual, every 8th hotspot-only.
		serviceType := "pppoe"
		switch {
		case i%8 == 0:
			serviceType = "hotspot"
		case i%5 == 0:
			serviceType = "dual"
		}
		batch = append(batch, []any{username, enc, name, phone, status,
			profileIDs[i%len(profileIDs)], nilUUID(ownerID), expires, serviceType})
	}

	for _, row := range batch {
		if _, err := db.Exec(ctx,
			`INSERT INTO subscribers
			   (username, password_enc, name, phone, status, profile_id, owner_manager_id,
			    expires_at, service_type, quota_cycle_anchor)
			 VALUES ($1,$2,$3,$4,$5,$6::uuid,$7::uuid,$8,$9, now())
			 ON CONFLICT (username) DO UPDATE SET
			    name=EXCLUDED.name, phone=EXCLUDED.phone, status=EXCLUDED.status,
			    profile_id=EXCLUDED.profile_id, owner_manager_id=EXCLUDED.owner_manager_id,
			    expires_at=EXCLUDED.expires_at, service_type=EXCLUDED.service_type`,
			row...); err != nil {
			return err
		}
	}
	return nil
}

// nilUUID returns nil for an empty id so the ::uuid cast stores NULL.
func nilUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
