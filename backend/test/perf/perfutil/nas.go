package perfutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func RandIP() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("10.%d.%d.%d", b[0], b[1], 1+int(b[2])%254)
}

func ProvisionNAS(ctx context.Context, db *pgxpool.Pool, typ string) (nasID, ip string, err error) {
	ip = RandIP()
	err = db.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc, type) VALUES ($1, $2::inet, $3, $4)
		 ON CONFLICT (ip) DO UPDATE SET type = EXCLUDED.type RETURNING id::text`,
		"perf-"+ip, ip, []byte("x"), typ).Scan(&nasID)
	return nasID, ip, err
}
