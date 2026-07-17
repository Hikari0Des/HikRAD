package radius

// Service-instance registry (FR-62, contract C3). A NAS runs many service
// instances — typically one PPPoE server plus a hotspot server per zone/SSID —
// and every Access-Request resolves to exactly one of them (C7), which decides
// the session's address pool (C6) and satisfies a subscriber's FR-64 scope.
//
// v1 modelled this as a single nas.type column, which could not describe a
// router doing PPPoE and hotspot at once — the gap this phase closes.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serviceRow is one nas_services record.
type serviceRow struct {
	ID              string
	NASID           string
	Service         string // pppoe | hotspot
	Label           string
	InterfaceNote   string
	IPPoolID        *string
	IPPoolName      string // resolved from ip_pools for the reply/snippet
	ROSServerName   string
	Enabled         bool
	ManagementMode  string // router | system (FR-67 / C5)
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// serviceView is the JSON read shape embedded in nasView (C3/C9; management_mode added by v2 phase 2 C9).
type serviceView struct {
	ID             string  `json:"id"`
	Service        string  `json:"service"`
	Label          string  `json:"label"`
	InterfaceNote  string  `json:"interface_note"`
	IPPoolID       *string `json:"ip_pool_id"`
	IPPoolName     string  `json:"ip_pool_name"`
	ROSServerName  string  `json:"ros_server_name"`
	Enabled        bool    `json:"enabled"`
	ManagementMode string  `json:"management_mode"`
	// LiveSessions is the FR-63 per-service session count the panel's services
	// sub-list shows. Filled by the API layer from C's live counter.
	LiveSessions int `json:"live_sessions"`
}

func (s serviceRow) view() serviceView {
	return serviceView{
		ID: s.ID, Service: s.Service, Label: s.Label, InterfaceNote: s.InterfaceNote,
		IPPoolID: s.IPPoolID, IPPoolName: s.IPPoolName,
		ROSServerName: s.ROSServerName, Enabled: s.Enabled, ManagementMode: s.ManagementMode,
	}
}

// serviceInput is one service instance in a NAS create/update body (C9).
type serviceInput struct {
	ID            string  `json:"id"` // empty on create; identifies the row on update
	Service       string  `json:"service"`
	Label         string  `json:"label"`
	InterfaceNote string  `json:"interface_note"`
	IPPoolID      *string `json:"ip_pool_id"`
	ROSServerName string  `json:"ros_server_name"`
	Enabled       *bool   `json:"enabled"` // omitted = true
}

func (s serviceInput) enabled() bool { return s.Enabled == nil || *s.Enabled }

const serviceColumns = `s.id::text, s.nas_id::text, s.service, s.label, s.interface_note,
	s.ip_pool_id::text, coalesce((SELECT name FROM ip_pools WHERE id = s.ip_pool_id), ''),
	s.ros_server_name, s.enabled, s.management_mode, s.created_at, s.updated_at`

func scanService(row pgx.Row) (serviceRow, error) {
	var s serviceRow
	err := row.Scan(&s.ID, &s.NASID, &s.Service, &s.Label, &s.InterfaceNote,
		&s.IPPoolID, &s.IPPoolName, &s.ROSServerName, &s.Enabled, &s.ManagementMode, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// listServices returns a NAS's service instances, ordered so the snippet and
// the panel list agree: PPPoE first, then hotspots by label.
func listServices(ctx context.Context, db *pgxpool.Pool, nasID string) ([]serviceRow, error) {
	return queryServices(ctx, db,
		`SELECT `+serviceColumns+` FROM nas_services s WHERE s.nas_id = $1::uuid
		  ORDER BY s.service DESC, s.label, s.id`, nasID)
}

// listServicesFor returns the service instances of many NAS ids in one query —
// the list endpoint's N+1 guard.
func listServicesFor(ctx context.Context, db *pgxpool.Pool, nasIDs []string) (map[string][]serviceRow, error) {
	if len(nasIDs) == 0 {
		return map[string][]serviceRow{}, nil
	}
	rows, err := queryServices(ctx, db,
		`SELECT `+serviceColumns+` FROM nas_services s WHERE s.nas_id = ANY($1::uuid[])
		  ORDER BY s.service DESC, s.label, s.id`, nasIDs)
	if err != nil {
		return nil, err
	}
	out := map[string][]serviceRow{}
	for _, r := range rows {
		out[r.NASID] = append(out[r.NASID], r)
	}
	return out, nil
}

// enabledServices returns a NAS's enabled instances — the candidate set the
// vendor adapter resolves an Access-Request against (C7).
func enabledServices(ctx context.Context, db *pgxpool.Pool, nasID string) ([]serviceRow, error) {
	return queryServices(ctx, db,
		`SELECT `+serviceColumns+` FROM nas_services s
		  WHERE s.nas_id = $1::uuid AND s.enabled
		  ORDER BY s.service DESC, s.label, s.id`, nasID)
}

func queryServices(ctx context.Context, db *pgxpool.Pool, q string, args ...any) ([]serviceRow, error) {
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []serviceRow
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// errServiceNotFound is returned when a service id doesn't belong to the NAS
// it was addressed under (FR-67 endpoints all scope by both ids).
var errServiceNotFound = errors.New("radius: nas service not found")

// getService fetches one nas_services row scoped to its NAS (FR-67 endpoints
// never let a service id from another NAS leak in).
func getService(ctx context.Context, db *pgxpool.Pool, nasID, serviceID string) (serviceRow, error) {
	rows, err := queryServices(ctx, db,
		`SELECT `+serviceColumns+` FROM nas_services s WHERE s.id = $1::uuid AND s.nas_id = $2::uuid`,
		serviceID, nasID)
	if err != nil {
		return serviceRow{}, err
	}
	if len(rows) == 0 {
		return serviceRow{}, errServiceNotFound
	}
	return rows[0], nil
}

// setServiceManagementMode flips a service's management_mode (FR-67.5 adopt).
// Never touches any other column — adopt writes nothing router-side, and this
// mirrors that on the HikRAD side too.
func setServiceManagementMode(ctx context.Context, db *pgxpool.Pool, nasID, serviceID, mode string) error {
	ct, err := db.Exec(ctx,
		`UPDATE nas_services SET management_mode = $3, updated_at = now()
		  WHERE id = $1::uuid AND nas_id = $2::uuid`,
		serviceID, nasID, mode)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errServiceNotFound
	}
	return nil
}

// systemServiceInput is what FR-67.3/67.4's create/edit flow persists after a
// successful ApplyService — always management_mode='system', whether this is
// a brand-new row or updating one already in that mode.
type systemServiceInput struct {
	Service       string
	Label         string
	InterfaceNote string
	IPPoolID      *string
	ROSServerName string
}

// upsertSystemService inserts a new system-managed service (serviceID == "")
// or updates an existing one (which FR-67.4 already required be management_mode
// = 'system' before allowing the plan/apply call that led here). Returns the
// row's id.
func upsertSystemService(ctx context.Context, db *pgxpool.Pool, nasID, serviceID string, in systemServiceInput) (string, error) {
	if serviceID == "" {
		var id string
		err := db.QueryRow(ctx,
			`INSERT INTO nas_services (nas_id, service, label, interface_note, ip_pool_id, ros_server_name, enabled, management_mode)
			 VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6, true, 'system')
			 RETURNING id::text`,
			nasID, in.Service, in.Label, in.InterfaceNote, in.IPPoolID, in.ROSServerName).Scan(&id)
		return id, err
	}
	ct, err := db.Exec(ctx,
		`UPDATE nas_services
		    SET service = $3, label = $4, interface_note = $5, ip_pool_id = $6::uuid,
		        ros_server_name = $7, management_mode = 'system', updated_at = now()
		  WHERE id = $1::uuid AND nas_id = $2::uuid`,
		serviceID, nasID, in.Service, in.Label, in.InterfaceNote, in.IPPoolID, in.ROSServerName)
	if err != nil {
		return "", err
	}
	if ct.RowsAffected() == 0 {
		return "", errServiceNotFound
	}
	return serviceID, nil
}

// errLastService is returned when a write would leave a NAS with no service
// instance. The C3 invariant ("every NAS has >= 1 service") is enforced here
// rather than in schema: a NAS with no service can authenticate nobody, so it
// is a silently dead router — better refused at the API with an explanation.
var errLastService = errors.New("radius: a NAS must keep at least one service")

// replaceServices reconciles a NAS's service instances against the submitted
// set inside tx: rows with a known id are updated, new ones inserted, and any
// omitted row deleted. Deleting is what makes the embedded array (C9) the whole
// truth for a NAS rather than an append-only list.
func replaceServices(ctx context.Context, tx pgx.Tx, nasID string, in []serviceInput) error {
	if len(in) == 0 {
		return errLastService
	}
	keep := make([]string, 0, len(in))
	for _, s := range in {
		var id string
		if s.ID != "" {
			err := tx.QueryRow(ctx,
				`UPDATE nas_services
				    SET service = $3, label = $4, interface_note = $5, ip_pool_id = $6::uuid,
				        ros_server_name = $7, enabled = $8
				  WHERE id = $1::uuid AND nas_id = $2::uuid
				 RETURNING id::text`,
				s.ID, nasID, s.Service, s.Label, s.InterfaceNote, s.IPPoolID,
				s.ROSServerName, s.enabled()).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				// The id is unknown or belongs to another NAS; treat the entry as
				// new rather than silently dropping the operator's service.
				s.ID = ""
			} else if err != nil {
				return err
			}
		}
		if s.ID == "" {
			if err := tx.QueryRow(ctx,
				`INSERT INTO nas_services (nas_id, service, label, interface_note, ip_pool_id, ros_server_name, enabled)
				 VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6, $7)
				 RETURNING id::text`,
				nasID, s.Service, s.Label, s.InterfaceNote, s.IPPoolID,
				s.ROSServerName, s.enabled()).Scan(&id); err != nil {
				return err
			}
		}
		keep = append(keep, id)
	}
	_, err := tx.Exec(ctx,
		`DELETE FROM nas_services WHERE nas_id = $1::uuid AND NOT (id = ANY($2::uuid[]))`,
		nasID, keep)
	return err
}
