package main

// Fixture provisioning: each scenario gets its own random NAS IP (and,
// where useful, subscriber) so concurrent/successive runs against a shared
// chaos DB never collide on the (nas_id, acct_session_id) space — mirrors
// internal/accounting/chaos_test.go's testIP()/insertTestNAS pattern.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randIP() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("10.%d.%d.%d", b[0], b[1], 1+int(b[2])%254)
}

// provisionNAS registers a NAS running one service instance of kind typ. Since
// v2 phase 1 the service lives on nas_services, not a nas.type column (FR-62).
func (r *Rig) provisionNAS(ctx context.Context, typ string) (nasID, ip string, err error) {
	ip = randIP()
	if err = r.db.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc) VALUES ($1, $2::inet, $3)
		 ON CONFLICT (ip) DO UPDATE SET name = EXCLUDED.name RETURNING id::text`,
		"chaos-"+ip, ip, []byte("x")).Scan(&nasID); err != nil {
		return "", "", err
	}
	_, err = r.db.Exec(ctx,
		`INSERT INTO nas_services (nas_id, service, label, enabled)
		 SELECT $1::uuid, $2, 'chaos', true
		  WHERE NOT EXISTS (SELECT 1 FROM nas_services WHERE nas_id = $1::uuid AND service = $2)`,
		nasID, typ)
	return nasID, ip, err
}
