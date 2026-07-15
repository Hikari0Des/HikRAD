// Command sizing is the NFR-3 evidence tool: bulk-generates synthetic
// usage_points at the 5k/2k reference load directly inside Postgres
// (generate_series, no round trips), measures real compressed/uncompressed
// bytes-per-row, force-compresses, and reads back the retention-policy
// config — then extrapolates to the full 12-month target so even a
// reduced-scale run produces a real, measurement-backed 200GB-tier sizing
// number instead of a back-of-envelope one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/test/perf/perfutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reference load (sub-PRD 03 / NFR-3): 2,000 concurrent sessions, 5-minute
// interims (12/hr), 12 months retention.
const (
	refSessions       = 2000
	refInterimsPerHr  = 12
	refMonths         = 12
)

func main() { os.Exit(run()) }

func run() int {
	dbURL := flag.String("db-url", envOr("HIKRAD_TEST_DB_URL", "postgres://hikrad:hikrad@localhost:5432/hikrad_sizing?sslmode=disable"), "Postgres URL")
	migrationsDir := flag.String("migrations", "migrations", "path to backend/migrations (relative to backend/, where this tool is meant to be run from)")
	pgContainer := flag.String("pg-container", "hikrad-sizing-postgres", "docker container name")
	provision := flag.Bool("provision", true, "start/stop a standalone Postgres container")
	months := flag.Int("months", 1, "months of synthetic data to actually generate (full-scale evidence: 12)")
	sessions := flag.Int("sessions", 200, "simulated concurrent sessions (full-scale evidence: 2000)")
	interimsPerHr := flag.Int("interims-per-hour", 12, "interims/hour per session (5-min default)")
	compressOlderThan := flag.String("compress-older-than", "0 days", "force-compress chunks older than this (production policy is 30 days; 0 days compresses everything generated so the smoke run still measures a real ratio)")
	out := flag.String("out", "../docs/evidence/raw/sizing.json", "JSON report path (relative to backend/)")
	flag.Parse()

	ctx := context.Background()

	if *provision {
		fmt.Println("== provisioning standalone Postgres for the sizing run ==")
		if err := perfutil.ProvisionPostgres(*pgContainer, *dbURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer func() { _ = perfutil.DockerRm(*pgContainer) }()
	}
	if err := perfutil.WaitTCP(perfutil.HostPort(*dbURL, "5432"), 60*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := perfutil.Migrate(*dbURL, *migrationsDir); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}
	db, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()

	intervalMinutes := 60 / *interimsPerHr
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}

	fmt.Printf("== generating %d months x %d sessions x %d interims/hr synthetic usage_points ==\n", *months, *sessions, *interimsPerHr)
	genStart := time.Now()
	var rows int64
	err = db.QueryRow(ctx, `
		WITH subs AS (
			SELECT gen_random_uuid() AS id FROM generate_series(1, $1) i
		), ticks AS (
			SELECT generate_series(now() - ($2 || ' months')::interval, now(), ($3 || ' minutes')::interval) AS t
		), ins AS (
			INSERT INTO usage_points (time, subscriber_id, nas_id, delta_down, delta_up, service)
			SELECT ticks.t, subs.id, gen_random_uuid(), (random()*2000)::bigint, (random()*1000)::bigint, 'pppoe'
			FROM subs CROSS JOIN ticks
			RETURNING 1
		)
		SELECT count(*) FROM ins`,
		*sessions, strconv.Itoa(*months), strconv.Itoa(intervalMinutes)).Scan(&rows)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		return 1
	}
	genElapsed := time.Since(genStart)
	fmt.Printf("generated %d rows in %s\n", rows, genElapsed)

	var rawBytes int64
	if err := db.QueryRow(ctx, `SELECT hypertable_size('usage_points')`).Scan(&rawBytes); err != nil {
		fmt.Fprintln(os.Stderr, "hypertable_size:", err)
		return 1
	}

	fmt.Println("== force-compressing generated chunks ==")
	compStart := time.Now()
	_, err = db.Exec(ctx, `SELECT compress_chunk(c, if_not_compressed => true) FROM show_chunks('usage_points', older_than => ($1)::interval) c`, *compressOlderThan)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compress_chunk:", err)
		return 1
	}
	compElapsed := time.Since(compStart)

	var beforeBytes, afterBytes int64
	err = db.QueryRow(ctx, `SELECT COALESCE(before_compression_total_bytes,0), COALESCE(after_compression_total_bytes,0) FROM hypertable_compression_stats('usage_points')`).
		Scan(&beforeBytes, &afterBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hypertable_compression_stats:", err)
		return 1
	}
	ratio := 0.0
	if afterBytes > 0 {
		ratio = float64(beforeBytes) / float64(afterBytes)
	}

	bytesPerRow := 0.0
	if rows > 0 {
		bytesPerRow = float64(rawBytes) / float64(rows)
	}
	targetRows := int64(refSessions) * int64(refInterimsPerHr) * 24 * 365
	projectedRawGB := bytesPerRow * float64(targetRows) / 1e9
	projectedCompressedGB := projectedRawGB
	if ratio > 0 {
		projectedCompressedGB = projectedRawGB / ratio
	}

	usagePointsRet, usageDailyRet, retErr := retentionDays(ctx, db)

	rep := map[string]any{
		"generated_months": *months, "generated_sessions": *sessions, "interims_per_hour": *interimsPerHr,
		"rows_generated": rows, "generate_seconds": genElapsed.Seconds(),
		"raw_size_bytes": rawBytes, "raw_size_human": humanBytes(rawBytes),
		"bytes_per_row": bytesPerRow,
		"compress_before_bytes": beforeBytes, "compress_after_bytes": afterBytes,
		"compression_ratio": ratio, "compress_seconds": compElapsed.Seconds(),
		"reference_load":            fmt.Sprintf("%d sessions x %d interims/hr x %d months", refSessions, refInterimsPerHr, refMonths),
		"projected_12mo_raw_gb":        projectedRawGB,
		"projected_12mo_compressed_gb": projectedCompressedGB,
		"nfr3_budget_gb":               200,
		"nfr3_headroom_ok":             projectedCompressedGB < 200,
		"usage_points_retention_days":  usagePointsRet,
		"usage_daily_retention_days":   usageDailyRet,
		"retention_floors_ok":          usagePointsRet >= 365 && usageDailyRet >= 1095,
		"retention_query_error":        errString(retErr),
		"generated_at":                 time.Now().UTC(),
	}

	fmt.Printf("\nmeasured: %.1f bytes/row, %.1fx compression\nprojected 12-month tier (2000 sessions, 12/hr): raw=%.2fGB compressed=%.2fGB (budget 200GB, headroom-ok=%v)\nretention: usage_points=%dd usage_daily=%dd (floors: >=365d / >=1095d) ok=%v\n",
		bytesPerRow, ratio, projectedRawGB, projectedCompressedGB, projectedCompressedGB < 200,
		usagePointsRet, usageDailyRet, usagePointsRet >= 365 && usageDailyRet >= 1095)

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err == nil {
		if f, err := os.Create(*out); err == nil {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rep)
			f.Close()
		}
	}

	if projectedCompressedGB >= 200 || usagePointsRet < 365 || usageDailyRet < 1095 {
		fmt.Println("[FAIL] NFR-3 sizing gate")
		return 1
	}
	fmt.Println("[PASS] NFR-3 sizing gate")
	return 0
}

var dropAfterRe = regexp.MustCompile(`(\d+)\s*days?`)

// retentionDays reads the add_retention_policy config for usage_points and
// usage_daily and extracts the configured day count, so the check is against
// the real running policy, not the migration source.
func retentionDays(ctx context.Context, db *pgxpool.Pool) (usagePoints, usageDaily int, err error) {
	rows, err := db.Query(ctx, `
		SELECT hypertable_name, config->>'drop_after'
		  FROM timescaledb_information.jobs
		 WHERE proc_name = 'policy_retention'
		   AND hypertable_name IN ('usage_points', 'usage_daily')`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var table, dropAfter string
		if err := rows.Scan(&table, &dropAfter); err != nil {
			return usagePoints, usageDaily, err
		}
		days := parseDays(dropAfter)
		switch table {
		case "usage_points":
			usagePoints = days
		case "usage_daily":
			usageDaily = days
		}
	}
	return usagePoints, usageDaily, rows.Err()
}

func parseDays(interval string) int {
	m := dropAfterRe.FindStringSubmatch(interval)
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
