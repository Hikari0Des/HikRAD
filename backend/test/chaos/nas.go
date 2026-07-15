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

func (r *Rig) provisionNAS(ctx context.Context, typ string) (nasID, ip string, err error) {
	ip = randIP()
	err = r.db.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc, type) VALUES ($1, $2::inet, $3, $4)
		 ON CONFLICT (ip) DO UPDATE SET type = EXCLUDED.type RETURNING id::text`,
		"chaos-"+ip, ip, []byte("x"), typ).Scan(&nasID)
	return nasID, ip, err
}
