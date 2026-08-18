package radius

// FR-145 Internet tab storage (nas_wan_uplinks, migration 0611). Mirrors
// nas_services.management_mode exactly (Decision 48): 'system' rows are
// HikRAD-created uplinks (editable, pushable); 'router' rows are ones
// discovered already configured (read-only reference, "pre-existing shown
// read-only" per the phase brief).

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type wanUplinkRow struct {
	ID             string
	NASID          string
	Interface      string
	Label          string
	Mode           string
	Address        string
	Gateway        string
	ManagementMode string
}

func listWANUplinks(ctx context.Context, db *pgxpool.Pool, nasID string) ([]wanUplinkRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, nas_id::text, interface, label, mode, address, gateway, management_mode
		   FROM nas_wan_uplinks WHERE nas_id = $1::uuid ORDER BY interface`, nasID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []wanUplinkRow
	for rows.Next() {
		var u wanUplinkRow
		if err := rows.Scan(&u.ID, &u.NASID, &u.Interface, &u.Label, &u.Mode, &u.Address, &u.Gateway, &u.ManagementMode); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// upsertSystemWANUplink persists a HikRAD-created uplink after a successful
// push — always management_mode='system', mirroring upsertSystemService.
func upsertSystemWANUplink(ctx context.Context, db *pgxpool.Pool, nasID, iface, label, mode, address, gateway string) error {
	_, err := db.Exec(ctx,
		`INSERT INTO nas_wan_uplinks (nas_id, interface, label, mode, address, gateway, management_mode)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, 'system')
		 ON CONFLICT (nas_id, interface) DO UPDATE SET
		    label = EXCLUDED.label, mode = EXCLUDED.mode, address = EXCLUDED.address,
		    gateway = EXCLUDED.gateway, management_mode = 'system', updated_at = now()`,
		nasID, iface, label, mode, address, gateway)
	return err
}

// adoptWANUplink imports a router-discovered uplink as a READ-ONLY
// reference. Never converts an existing 'system' row (mirrors adoptPool).
func adoptWANUplink(ctx context.Context, db *pgxpool.Pool, nasID, iface, mode, address, gateway string) error {
	_, err := db.Exec(ctx,
		`INSERT INTO nas_wan_uplinks (nas_id, interface, mode, address, gateway, management_mode)
		 VALUES ($1::uuid, $2, $3, $4, $5, 'router')
		 ON CONFLICT (nas_id, interface) DO UPDATE SET
		    mode = EXCLUDED.mode, address = EXCLUDED.address, gateway = EXCLUDED.gateway, updated_at = now()
		  WHERE nas_wan_uplinks.management_mode = 'router'`,
		nasID, iface, mode, address, gateway)
	return err
}
