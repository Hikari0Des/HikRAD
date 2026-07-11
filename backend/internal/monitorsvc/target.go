package monitorsvc

// Probe targets. A target is either a NAS (B's read-only `nas` table) or a
// monitored device (our `monitored_devices`, FR-60). The probe engine treats
// both identically — same scheduler, same state machine, same history table —
// differing only in which id column a probe row carries and which alert rule
// type a transition fires. SNMP community is decrypted here via A's crypto so
// the engine never handles ciphertext.

import (
	"context"

	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

// targetKind distinguishes the two probe target lists.
type targetKind string

const (
	kindNAS    targetKind = "nas"
	kindDevice targetKind = "device"
)

// target is one thing the engine probes.
type target struct {
	kind      targetKind
	id        string
	name      string
	ip        string
	community string // decrypted SNMP v2c community, "" → ICMP only
}

// nasID/deviceID return the id for the matching health_probes column ("" for the
// other kind), so probe writes stay a single code path.
func (t target) nasID() *string {
	if t.kind == kindNAS {
		return &t.id
	}
	return nil
}
func (t target) deviceID() *string {
	if t.kind == kindDevice {
		return &t.id
	}
	return nil
}

// loadTargets reads every enabled NAS and monitored device as probe targets.
// SNMP community decryption failures are non-fatal: the target degrades to
// ICMP-only rather than dropping out of monitoring.
func loadTargets(ctx context.Context, db *pgxpool.Pool) ([]target, error) {
	var out []target

	nasRows, err := db.Query(ctx,
		`SELECT id::text, name, host(ip), snmp_community_enc FROM nas WHERE enabled`)
	if err != nil {
		return nil, err
	}
	for nasRows.Next() {
		t := target{kind: kindNAS}
		var enc []byte
		if err := nasRows.Scan(&t.id, &t.name, &t.ip, &enc); err != nil {
			nasRows.Close()
			return nil, err
		}
		t.community = decryptCommunity(enc)
		out = append(out, t)
	}
	nasRows.Close()
	if err := nasRows.Err(); err != nil {
		return nil, err
	}

	devRows, err := db.Query(ctx,
		`SELECT id::text, name, host(ip), snmp_community_enc FROM monitored_devices WHERE enabled`)
	if err != nil {
		return nil, err
	}
	for devRows.Next() {
		t := target{kind: kindDevice}
		var enc []byte
		if err := devRows.Scan(&t.id, &t.name, &t.ip, &enc); err != nil {
			devRows.Close()
			return nil, err
		}
		t.community = decryptCommunity(enc)
		out = append(out, t)
	}
	devRows.Close()
	if err := devRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// decryptCommunity opens an SNMP community ciphertext, returning "" for a null
// column or any decryption failure (best-effort enrichment).
func decryptCommunity(enc []byte) string {
	if len(enc) == 0 {
		return ""
	}
	plain, err := crypto.Decrypt(enc)
	if err != nil {
		return ""
	}
	return string(plain)
}
