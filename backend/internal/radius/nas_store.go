package radius

// NAS registry storage (FR-13) and the enabled-NAS cache the authorize path's
// known-NAS check reads. Secrets and SNMP communities are AES-GCM sealed via
// platform/crypto (C3) and never leave this package in cleartext except when
// regenerating the FreeRADIUS clients file (clients.go) or a permission-gated
// reveal.

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nasRow is the full NAS record. Secret/SNMP/API-password are stored sealed;
// callers that need cleartext decrypt explicitly.
type nasRow struct {
	ID             string
	Name           string
	IP             string
	SecretEnc      []byte
	Vendor         string
	CoAPort        int
	SNMPEnc        []byte
	ROSVersion     *string
	Location       string
	Enabled        bool
	APIPort        int
	APIUser        string
	APIPasswordEnc []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// nasView is the JSON read shape (C7-B, amended by v2 phase-1 C3/C9: the
// top-level `type` is gone — a NAS runs many services, listed in Services).
// Secrets are never serialized.
type nasView struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	IP         string        `json:"ip"`
	Services   []serviceView `json:"services"`
	Vendor     string        `json:"vendor"`
	CoAPort    int           `json:"coa_port"`
	HasSNMP    bool          `json:"has_snmp"`
	ROSVersion *string       `json:"ros_version"`
	Location   string        `json:"location"`
	Enabled    bool          `json:"enabled"`
	// API* is the FR-56.2 auto-setup credential slice: HasAPICreds reports
	// whether a password is on file without ever serializing it.
	APIPort     int       `json:"api_port"`
	APIUser     string    `json:"api_user"`
	HasAPICreds bool      `json:"has_api_creds"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// view renders the NAS without its services; callers that serve JSON attach
// them via withServices (one query for a list, rather than N).
func (n nasRow) view() nasView {
	return nasView{
		ID: n.ID, Name: n.Name, IP: n.IP, Vendor: n.Vendor,
		Services: []serviceView{},
		CoAPort:  n.CoAPort, HasSNMP: len(n.SNMPEnc) > 0, ROSVersion: n.ROSVersion,
		Location: n.Location, Enabled: n.Enabled,
		APIPort: n.APIPort, APIUser: n.APIUser, HasAPICreds: len(n.APIPasswordEnc) > 0,
		CreatedAt: n.CreatedAt.UTC(), UpdatedAt: n.UpdatedAt.UTC(),
	}
}

const nasColumns = `id::text, name, host(ip) AS ip, secret_enc, vendor,
	coa_port, snmp_community_enc, ros_version, location, enabled,
	api_port, coalesce(api_user, ''), api_password_enc, created_at, updated_at`

func scanNAS(row pgx.Row) (nasRow, error) {
	var n nasRow
	err := row.Scan(&n.ID, &n.Name, &n.IP, &n.SecretEnc, &n.Vendor,
		&n.CoAPort, &n.SNMPEnc, &n.ROSVersion, &n.Location, &n.Enabled,
		&n.APIPort, &n.APIUser, &n.APIPasswordEnc, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}

func listNAS(ctx context.Context, db *pgxpool.Pool) ([]nasRow, error) {
	rows, err := db.Query(ctx, `SELECT `+nasColumns+` FROM nas ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []nasRow
	for rows.Next() {
		n, err := scanNAS(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func getNAS(ctx context.Context, db *pgxpool.Pool, id string) (nasRow, error) {
	return scanNAS(db.QueryRow(ctx, `SELECT `+nasColumns+` FROM nas WHERE id = $1`, id))
}

type nasInput struct {
	Name       string
	IP         string
	Secret     string
	Vendor     string
	CoAPort    int
	SNMP       string
	ROSVersion *string
	Location   string
	Enabled    bool
	// APIPort/APIUser/APIPassword are the FR-56.2 auto-setup credentials.
	// APIPassword empty means "leave untouched" on update (rotateAPIPassword
	// gates whether it's written at all, matching Secret/SNMP's convention).
	APIPort     int
	APIUser     string
	APIPassword string
}

func insertNAS(ctx context.Context, q pgxQuerier, in nasInput) (nasRow, error) {
	secretEnc, err := crypto.Encrypt([]byte(in.Secret))
	if err != nil {
		return nasRow{}, err
	}
	var snmpEnc []byte
	if in.SNMP != "" {
		if snmpEnc, err = crypto.Encrypt([]byte(in.SNMP)); err != nil {
			return nasRow{}, err
		}
	}
	var apiPasswordEnc []byte
	if in.APIPassword != "" {
		if apiPasswordEnc, err = crypto.Encrypt([]byte(in.APIPassword)); err != nil {
			return nasRow{}, err
		}
	}
	row := q.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc, vendor, coa_port, snmp_community_enc, ros_version, location, enabled, api_port, api_user, api_password_enc)
		 VALUES ($1, $2::inet, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), $12)
		 RETURNING `+nasColumns,
		in.Name, in.IP, secretEnc, in.Vendor, in.CoAPort, snmpEnc, in.ROSVersion, in.Location, in.Enabled,
		apiPortOrDefault(in.APIPort), in.APIUser, apiPasswordEnc)
	return scanNAS(row)
}

// updateNAS applies a full update. A nil secret/snmp/api-password leaves the
// sealed value untouched (the reveal/rotate flow sets them explicitly).
func updateNAS(ctx context.Context, q pgxQuerier, id string, in nasInput, rotateSecret, rotateSNMP, rotateAPIPassword bool) (nasRow, error) {
	var secretEnc []byte
	if rotateSecret {
		var err error
		if secretEnc, err = crypto.Encrypt([]byte(in.Secret)); err != nil {
			return nasRow{}, err
		}
	}
	var snmpEnc []byte
	if rotateSNMP && in.SNMP != "" {
		var err error
		if snmpEnc, err = crypto.Encrypt([]byte(in.SNMP)); err != nil {
			return nasRow{}, err
		}
	}
	var apiPasswordEnc []byte
	if rotateAPIPassword && in.APIPassword != "" {
		var err error
		if apiPasswordEnc, err = crypto.Encrypt([]byte(in.APIPassword)); err != nil {
			return nasRow{}, err
		}
	}
	row := q.QueryRow(ctx,
		`UPDATE nas SET
		    name = $2, ip = $3::inet, vendor = $4, coa_port = $5,
		    ros_version = $6, location = $7, enabled = $8,
		    secret_enc = COALESCE($9, secret_enc),
		    snmp_community_enc = CASE WHEN $10 THEN $11 ELSE snmp_community_enc END,
		    api_port = $12, api_user = NULLIF($13, ''),
		    api_password_enc = CASE WHEN $14 THEN $15 ELSE api_password_enc END
		 WHERE id = $1
		 RETURNING `+nasColumns,
		id, in.Name, in.IP, in.Vendor, in.CoAPort, in.ROSVersion, in.Location, in.Enabled,
		secretEnc, rotateSNMP, snmpEnc,
		apiPortOrDefault(in.APIPort), in.APIUser, rotateAPIPassword, apiPasswordEnc)
	return scanNAS(row)
}

// pgxQuerier is the subset of pgxpool.Pool / pgx.Tx the NAS writes need, so a
// create can insert the NAS and its service instances (C3) in one transaction —
// a NAS committed without its services would authenticate nobody.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func apiPortOrDefault(p int) int {
	if p == 0 {
		return 8728
	}
	return p
}

func deleteNAS(ctx context.Context, db *pgxpool.Pool, id string) error {
	ct, err := db.Exec(ctx, `DELETE FROM nas WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// --- enabled-NAS cache for the authorize known-NAS check -------------------

const nasCacheTTL = 10 * time.Second

type nasRegistry struct {
	db  *pgxpool.Pool
	log *slog.Logger

	mu       sync.RWMutex
	byIP     map[string]struct{}
	loaded   bool
	loadedAt time.Time
}

func newNASRegistry(db *pgxpool.Pool, log *slog.Logger) *nasRegistry {
	return &nasRegistry{db: db, log: log, byIP: map[string]struct{}{}}
}

// known reports whether an enabled NAS with source IP ip is registered. The
// snapshot is cached for nasCacheTTL and refreshed on CRUD (invalidate); a DB
// error after a prior successful load falls back to the last snapshot so a
// transient blip cannot black-hole all auth (NFR-1 resilience).
func (r *nasRegistry) known(ctx context.Context, ip string) (bool, error) {
	key := canonicalIP(ip)
	r.mu.RLock()
	fresh := r.loaded && time.Since(r.loadedAt) < nasCacheTTL
	_, ok := r.byIP[key]
	hadSnapshot := r.loaded
	r.mu.RUnlock()
	if fresh {
		return ok, nil
	}
	if err := r.reload(ctx); err != nil {
		if hadSnapshot {
			r.log.Warn("radius: nas registry reload failed; using stale snapshot", "error", err)
			return ok, nil
		}
		return false, err
	}
	r.mu.RLock()
	_, ok = r.byIP[key]
	r.mu.RUnlock()
	return ok, nil
}

func (r *nasRegistry) reload(ctx context.Context) error {
	rows, err := r.db.Query(ctx, `SELECT host(ip) FROM nas WHERE enabled`)
	if err != nil {
		return err
	}
	defer rows.Close()
	next := map[string]struct{}{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return err
		}
		next[canonicalIP(ip)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	r.byIP = next
	r.loaded = true
	r.loadedAt = time.Now()
	r.mu.Unlock()
	return nil
}

// invalidate forces the next known() call to reload. Called after every NAS
// mutation so a newly-created NAS authenticates with no restart (AC-13a).
func (r *nasRegistry) invalidate() {
	r.mu.Lock()
	r.loaded = false
	r.mu.Unlock()
}

// canonicalIP normalizes an address so "10.0.0.1" from a RADIUS packet and the
// DB's host(ip) rendering compare equal regardless of formatting.
func canonicalIP(s string) string {
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}

// decryptToString opens a sealed secret to its cleartext string (config-snippet
// reveal, clients regeneration). Empty ciphertext yields "".
func decryptToString(enc []byte) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	plain, err := crypto.Decrypt(enc)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
