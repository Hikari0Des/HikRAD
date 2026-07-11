package monitorsvc

// Monitored-device CRUD (FR-60, contract C5). Devices are a second probe target
// list, entirely separate from the NAS registry: they are never FreeRADIUS
// clients and never appear in NAS screens. The probe engine reloads targets on
// its own timer, so a create/update/delete here is picked up within one reload
// cycle without any cross-process signalling. SNMP community is sealed via A's
// crypto exactly like nas.snmp_community_enc.

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5"
)

type deviceView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	IP         string    `json:"ip"`
	Type       string    `json:"type"`
	HasSNMP    bool      `json:"has_snmp"` // whether a community is set (never the value)
	Location   string    `json:"location"`
	Notes      string    `json:"notes"`
	Enabled    bool      `json:"enabled"`
	Status     string    `json:"status"` // current reachability (up|down|unknown)
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type deviceInput struct {
	Name          string  `json:"name" validate:"required,min=1,max=128"`
	IP            string  `json:"ip" validate:"required,ip"`
	Type          string  `json:"type" validate:"omitempty,oneof=ap switch router server other"`
	SNMPCommunity *string `json:"snmp_community"` // null=leave/unset, "" on update clears
	Location      string  `json:"location" validate:"omitempty,max=256"`
	Notes         string  `json:"notes" validate:"omitempty,max=1024"`
	Enabled       *bool   `json:"enabled"`
}

func listDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := pkgDB.Query(ctx,
		`SELECT id::text, name, host(ip), type, snmp_community_enc IS NOT NULL,
		        location, notes, enabled, created_at, updated_at
		   FROM monitored_devices ORDER BY name`)
	if err != nil {
		internalErr(w, "list devices", err)
		return
	}
	defer rows.Close()
	items := make([]deviceView, 0, 32)
	for rows.Next() {
		var d deviceView
		if err := rows.Scan(&d.ID, &d.Name, &d.IP, &d.Type, &d.HasSNMP,
			&d.Location, &d.Notes, &d.Enabled, &d.CreatedAt, &d.UpdatedAt); err != nil {
			internalErr(w, "scan device", err)
			return
		}
		d.CreatedAt = d.CreatedAt.UTC()
		d.UpdatedAt = d.UpdatedAt.UTC()
		d.Status = deviceStatus(ctx, d.ID)
		items = append(items, d)
	}
	if rows.Err() != nil {
		internalErr(w, "rows devices", rows.Err())
		return
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, ""))
}

func getDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	d, err := readDevice(ctx, id)
	if err == pgx.ErrNoRows {
		httpapi.Error(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	if err != nil {
		internalErr(w, "get device", err)
		return
	}
	d.Status = deviceStatus(ctx, d.ID)
	httpapi.JSON(w, http.StatusOK, d)
}

func createDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in deviceInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	typ := in.Type
	if typ == "" {
		typ = "other"
	}
	enabled := in.Enabled == nil || *in.Enabled
	var enc []byte
	if in.SNMPCommunity != nil && *in.SNMPCommunity != "" {
		sealed, err := crypto.Encrypt([]byte(*in.SNMPCommunity))
		if err != nil {
			internalErr(w, "seal community", err)
			return
		}
		enc = sealed
	}

	var id string
	err := pkgDB.QueryRow(ctx,
		`INSERT INTO monitored_devices (name, ip, type, snmp_community_enc, location, notes, enabled)
		 VALUES ($1, $2::inet, $3, $4, $5, $6, $7) RETURNING id::text`,
		in.Name, in.IP, typ, enc, in.Location, in.Notes, enabled).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			httpapi.Error(w, http.StatusConflict, "conflict", "a device with this IP already exists")
			return
		}
		internalErr(w, "insert device", err)
		return
	}
	_ = auth.Audit(ctx, "device.create", "monitored_device", id, nil, map[string]any{"name": in.Name, "ip": in.IP})
	d, _ := readDevice(ctx, id)
	httpapi.JSON(w, http.StatusCreated, d)
}

func updateDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	before, err := readDevice(ctx, id)
	if err == pgx.ErrNoRows {
		httpapi.Error(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	if err != nil {
		internalErr(w, "load device", err)
		return
	}
	var in deviceInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	typ := in.Type
	if typ == "" {
		typ = before.Type
	}
	enabled := before.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	// snmp_community: nil = leave as-is; "" = clear; value = replace.
	setCommunity := in.SNMPCommunity != nil
	var enc []byte
	if setCommunity && *in.SNMPCommunity != "" {
		sealed, serr := crypto.Encrypt([]byte(*in.SNMPCommunity))
		if serr != nil {
			internalErr(w, "seal community", serr)
			return
		}
		enc = sealed
	}
	_, err = pkgDB.Exec(ctx,
		`UPDATE monitored_devices
		    SET name=$2, ip=$3::inet, type=$4, location=$5, notes=$6, enabled=$7,
		        snmp_community_enc = CASE WHEN $8 THEN $9 ELSE snmp_community_enc END
		  WHERE id=$1::uuid`,
		id, in.Name, in.IP, typ, in.Location, in.Notes, enabled, setCommunity, enc)
	if err != nil {
		if isUniqueViolation(err) {
			httpapi.Error(w, http.StatusConflict, "conflict", "a device with this IP already exists")
			return
		}
		internalErr(w, "update device", err)
		return
	}
	_ = auth.Audit(ctx, "device.update", "monitored_device", id,
		map[string]any{"name": before.Name, "ip": before.IP},
		map[string]any{"name": in.Name, "ip": in.IP})
	d, _ := readDevice(ctx, id)
	d.Status = deviceStatus(ctx, id)
	httpapi.JSON(w, http.StatusOK, d)
}

func deleteDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	ct, err := pkgDB.Exec(ctx, `DELETE FROM monitored_devices WHERE id=$1::uuid`, id)
	if err != nil {
		internalErr(w, "delete device", err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpapi.Error(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	_ = auth.Audit(ctx, "device.delete", "monitored_device", id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func readDevice(ctx context.Context, id string) (deviceView, error) {
	var d deviceView
	err := pkgDB.QueryRow(ctx,
		`SELECT id::text, name, host(ip), type, snmp_community_enc IS NOT NULL,
		        location, notes, enabled, created_at, updated_at
		   FROM monitored_devices WHERE id=$1::uuid`, id).
		Scan(&d.ID, &d.Name, &d.IP, &d.Type, &d.HasSNMP, &d.Location, &d.Notes, &d.Enabled, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return deviceView{}, err
	}
	d.CreatedAt = d.CreatedAt.UTC()
	d.UpdatedAt = d.UpdatedAt.UTC()
	return d, nil
}
