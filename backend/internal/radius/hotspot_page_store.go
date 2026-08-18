package radius

// FR-148 hotspot login page tracking (migration 0613). See 0613's comment
// for why pushed_version/pushed_ros_version are tracked separately from
// design_version: the page lives ONLY on router flash, and a RouterOS
// upgrade can wipe it, so "unpushed edit" and "stale after an upgrade" are
// two distinct, separately-tellable reasons the panel must be able to warn
// about rather than one generic "might be stale".

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type hotspotPageRow struct {
	ServiceID        string
	Mode             string // router_default | branded_template | custom
	CustomHTML       string
	DesignVersion    int
	PushedVersion    *int
	PushedAt         *time.Time
	PushedROSVersion string
	Found            bool
}

func getHotspotPage(ctx context.Context, db *pgxpool.Pool, serviceID string) (hotspotPageRow, error) {
	var row hotspotPageRow
	err := db.QueryRow(ctx,
		`SELECT mode, coalesce(custom_html, ''), design_version, pushed_version, pushed_at, coalesce(pushed_ros_version, '')
		   FROM nas_hotspot_pages WHERE service_id = $1::uuid`, serviceID).
		Scan(&row.Mode, &row.CustomHTML, &row.DesignVersion, &row.PushedVersion, &row.PushedAt, &row.PushedROSVersion)
	if err == pgx.ErrNoRows {
		return hotspotPageRow{ServiceID: serviceID, Mode: "router_default"}, nil
	}
	if err != nil {
		return hotspotPageRow{}, err
	}
	row.ServiceID = serviceID
	row.Found = true
	return row, nil
}

// saveHotspotPageDesign upserts the design (mode + custom HTML), bumping
// design_version — a pure HikRAD-side save, no router write. Switching TO
// router_default resets design_version to 0 and clears any pushed record:
// there is nothing of HikRAD's left to call stale once the operator has
// chosen to leave the router's own page alone.
func saveHotspotPageDesign(ctx context.Context, db *pgxpool.Pool, serviceID, nasID, mode, customHTML string) error {
	if mode == "router_default" {
		_, err := db.Exec(ctx,
			`INSERT INTO nas_hotspot_pages (service_id, nas_id, mode, custom_html, design_version, pushed_version, pushed_at, pushed_ros_version)
			 VALUES ($1::uuid, $2::uuid, 'router_default', NULL, 0, NULL, NULL, NULL)
			 ON CONFLICT (service_id) DO UPDATE SET
			    mode = 'router_default', custom_html = NULL, design_version = 0,
			    pushed_version = NULL, pushed_at = NULL, pushed_ros_version = NULL, updated_at = now()`,
			serviceID, nasID)
		return err
	}
	_, err := db.Exec(ctx,
		`INSERT INTO nas_hotspot_pages (service_id, nas_id, mode, custom_html, design_version)
		 VALUES ($1::uuid, $2::uuid, $3, $4, 1)
		 ON CONFLICT (service_id) DO UPDATE SET
		    mode = EXCLUDED.mode, custom_html = EXCLUDED.custom_html,
		    design_version = nas_hotspot_pages.design_version + 1, updated_at = now()`,
		serviceID, nasID, mode, customHTML)
	return err
}

// markHotspotPagePushed records a successful push (FR-148): which design
// version reached the router, and at which RouterOS version — the two
// independent staleness signals (unpushed edit vs. upgrade-since-push).
func markHotspotPagePushed(ctx context.Context, db *pgxpool.Pool, serviceID string, version int, rosVersion string) error {
	_, err := db.Exec(ctx,
		`UPDATE nas_hotspot_pages SET pushed_version = $2, pushed_at = now(), pushed_ros_version = $3, updated_at = now()
		  WHERE service_id = $1::uuid`, serviceID, version, rosVersion)
	return err
}

// hotspotPageStale reports whether the router-served page (if any) may not
// match the currently-saved design — either because the design has been
// edited since the last push, or because the router's RouterOS version has
// changed since the last push (an upgrade that may have wiped flash).
// currentROSVersion empty (never sampled) does not itself mean stale — there
// is nothing to compare against yet.
func (row hotspotPageRow) hotspotPageStale(currentROSVersion string) (stale bool, reason string) {
	if row.Mode == "router_default" {
		return false, ""
	}
	if row.PushedVersion == nil {
		return true, "never_pushed"
	}
	if *row.PushedVersion != row.DesignVersion {
		return true, "unpushed_edit"
	}
	if currentROSVersion != "" && row.PushedROSVersion != "" && currentROSVersion != row.PushedROSVersion {
		return true, "ros_upgraded_since_push"
	}
	return false, ""
}
