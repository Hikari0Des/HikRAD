package subscribers_test

// Gate item 1: the v2 phase-1 migrations are LOSSLESS against real v1 data.
//
// This is the test that matters most in the phase: 0500 and 0501 both drop a
// column after backfilling from it, so a mistake is unrecoverable on a customer
// database — the old value is simply gone. Every other test in the suite runs
// against a schema that is already at head and therefore cannot see the
// backfill at all.
//
// It works by migrating a THROWAWAY database to the last v1 migration (0412),
// writing rows in v1's shape (allow_hotspot bool, nas.type), then migrating to
// head and asserting every row mapped correctly with none lost. Gated on
// HIKRAD_TEST_DB_URL like the rest of the DB suite.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// lastV1Migration is the highest migration that existed before this phase
// (v1.1's has_password). Migrating to exactly this version reproduces a real
// v1 install's schema.
const lastV1Migration = 412

// headMigration is the highest migration this phase adds.
const headMigration = 504

// preScopeSetMigration is 0503 — the last version at which the FR-64 scope was
// still the single (nas_id, nas_service_id) pair on the row, before 0504 moved
// it to a set. TestNASScopeSetMigrationLossless migrates to exactly this, writes
// pair-shaped rows, then upgrades.
const preScopeSetMigration = 503

// migrateURL rewrites a postgres:// URL to the pgx5:// scheme golang-migrate's
// driver registers under, mirroring platform.Migrate.
func migrateURL(dbURL string) string {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dbURL, scheme) {
			return "pgx5://" + strings.TrimPrefix(dbURL, scheme)
		}
	}
	return dbURL
}

// withScratchDB creates a throwaway database on the same server as
// HIKRAD_TEST_DB_URL and returns its URL. The shared test DB is already at head,
// so the backfill can only be observed on a database of our own.
func withScratchDB(t *testing.T) string {
	t.Helper()
	base := os.Getenv("HIKRAD_TEST_DB_URL")
	if base == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping migration suite")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse HIKRAD_TEST_DB_URL: %v", err)
	}
	name := fmt.Sprintf("hikrad_mig_%d", rand.Int31())

	admin := *u
	admin.Path = "/postgres"
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, admin.String())
	if err != nil {
		t.Skipf("cannot reach the postgres maintenance database: %v", err)
	}
	defer adminPool.Close()
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Skipf("cannot create a scratch database (needs CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		c, err := pgxpool.New(context.Background(), admin.String())
		if err != nil {
			return
		}
		defer c.Close()
		// Terminate stragglers so the DROP cannot block the suite.
		_, _ = c.Exec(context.Background(),
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
		_, _ = c.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name)
	})

	scratch := *u
	scratch.Path = "/" + name
	return scratch.String()
}

func migrator(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()
	dir, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+filepath.ToSlash(dir), migrateURL(dbURL))
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	return m
}

func TestServiceTypeMigrationLossless(t *testing.T) {
	dbURL := withScratchDB(t)
	ctx := context.Background()
	_ = slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Bring the scratch DB to a real v1 install's schema.
	m := migrator(t, dbURL)
	if err := m.Migrate(lastV1Migration); err != nil {
		t.Fatalf("migrate to v1 head (%d): %v", lastV1Migration, err)
	}
	srcErr, dbErr := m.Close()
	if srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect scratch: %v", err)
	}
	defer pool.Close()

	// 2. Write rows in v1's shape. Mixed allow_hotspot, and both NAS types —
	// exactly what an upgrading customer's database holds.
	subs := []struct {
		username     string
		allowHotspot bool
		want         string
	}{
		{"v1_ppp_a", false, "pppoe"},
		{"v1_ppp_b", false, "pppoe"},
		{"v1_hs_a", true, "dual"}, // v1's true meant PPPoE *plus* hotspot (FR-58)
		{"v1_hs_b", true, "dual"},
	}
	for _, s := range subs {
		if _, err := pool.Exec(ctx,
			`INSERT INTO subscribers (username, password_enc, status, allow_hotspot)
			 VALUES ($1, '\x0102'::bytea, 'active', $2)`,
			s.username, s.allowHotspot); err != nil {
			t.Fatalf("insert %s: %v", s.username, err)
		}
	}
	nases := []struct{ name, ip, typ string }{
		{"v1-ppp-nas", "10.77.0.1", "pppoe"},
		{"v1-hs-nas", "10.77.0.2", "hotspot"},
	}
	for _, n := range nases {
		if _, err := pool.Exec(ctx,
			`INSERT INTO nas (name, ip, secret_enc, type) VALUES ($1, $2::inet, '\x01'::bytea, $3)`,
			n.name, n.ip, n.typ); err != nil {
			t.Fatalf("insert nas %s: %v", n.name, err)
		}
	}

	var subsBefore, nasBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subscribers`).Scan(&subsBefore); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM nas`).Scan(&nasBefore); err != nil {
		t.Fatal(err)
	}

	// 3. Upgrade to this phase's head — what `hikrad update` does on boot.
	m2 := migrator(t, dbURL)
	if err := m2.Migrate(headMigration); err != nil {
		t.Fatalf("migrate to v2 phase-1 head (%d): %v", headMigration, err)
	}
	if v, dirty, err := m2.Version(); err != nil || dirty || v != headMigration {
		t.Fatalf("post-migration version = %d dirty=%v err=%v; want %d clean", v, dirty, err, headMigration)
	}
	srcErr, dbErr = m2.Close()
	if srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	// 4a. Zero row loss.
	var subsAfter, nasAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subscribers`).Scan(&subsAfter); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM nas`).Scan(&nasAfter); err != nil {
		t.Fatal(err)
	}
	if subsAfter != subsBefore {
		t.Errorf("subscribers: %d rows before, %d after — the migration lost rows", subsBefore, subsAfter)
	}
	if nasAfter != nasBefore {
		t.Errorf("nas: %d rows before, %d after — the migration lost rows", nasBefore, nasAfter)
	}

	// 4b. Every allow_hotspot bit mapped to the right service_type.
	for _, s := range subs {
		var got string
		if err := pool.QueryRow(ctx,
			`SELECT service_type FROM subscribers WHERE username = $1`, s.username).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", s.username, err)
		}
		if got != s.want {
			t.Errorf("%s: allow_hotspot=%v backfilled to service_type=%q, want %q",
				s.username, s.allowHotspot, got, s.want)
		}
	}

	// 4c. The retired column is gone (the drop half of 0500).
	var hasAllowHotspot bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		                 WHERE table_name='subscribers' AND column_name='allow_hotspot')`).
		Scan(&hasAllowHotspot); err != nil {
		t.Fatal(err)
	}
	if hasAllowHotspot {
		t.Error("subscribers.allow_hotspot still exists after 0500")
	}

	// 4d. Exactly one nas_services row per NAS, carrying its old type — the
	// single-service shape that keeps a v1 install's auth behaviour identical.
	for _, n := range nases {
		rows, err := pool.Query(ctx,
			`SELECT s.service, s.enabled FROM nas_services s
			   JOIN nas ON nas.id = s.nas_id WHERE nas.name = $1`, n.name)
		if err != nil {
			t.Fatal(err)
		}
		var services []string
		for rows.Next() {
			var svc string
			var enabled bool
			if err := rows.Scan(&svc, &enabled); err != nil {
				t.Fatal(err)
			}
			if !enabled {
				t.Errorf("%s: backfilled service %q is disabled; it must inherit the NAS's enabled state", n.name, svc)
			}
			services = append(services, svc)
		}
		rows.Close()
		if len(services) != 1 || services[0] != n.typ {
			t.Errorf("%s (v1 type=%q): backfilled services = %v, want exactly [%s]",
				n.name, n.typ, services, n.typ)
		}
	}

	// 4e. nas.type is gone (the drop half of 0501).
	var hasNASType bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		                 WHERE table_name='nas' AND column_name='type')`).Scan(&hasNASType); err != nil {
		t.Fatal(err)
	}
	if hasNASType {
		t.Error("nas.type still exists after 0501")
	}

	// 4f. FR-64 scope defaults to "any NAS" for every migrated row: no v1 row had
	// a scope, so none may come out with one. A migrated subscriber that landed
	// scoped would silently stop authenticating everywhere they used to.
	var scoped int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subscriber_nas_scopes`).Scan(&scoped); err != nil {
		t.Fatal(err)
	}
	if scoped != 0 {
		t.Errorf("%d migrated subscribers came out NAS-scoped; every v1 row must stay any-NAS", scoped)
	}
}

// TestNASScopeSetMigrationLossless covers 0504, which has the same
// unrecoverable shape as 0500/0501: it backfills the single (nas_id,
// nas_service_id) pair into the scope table and then DROPS the columns. A
// mistake silently widens a deliberately-restricted account to every NAS on the
// operator's network, which nothing later can detect — the old value is gone.
func TestNASScopeSetMigrationLossless(t *testing.T) {
	dbURL := withScratchDB(t)
	ctx := context.Background()

	// 1. Stop at 0503, where the scope is still a pair of columns.
	m := migrator(t, dbURL)
	if err := m.Migrate(preScopeSetMigration); err != nil {
		t.Fatalf("migrate to %d: %v", preScopeSetMigration, err)
	}
	if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect scratch: %v", err)
	}
	defer pool.Close()

	// 2. A NAS with two service instances, and subscribers scoped three ways:
	// unscoped, NAS-wide, and pinned to one instance.
	var nasID, svcA, svcB string
	if err := pool.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc) VALUES ('scope-nas', '10.78.0.1'::inet, '\x01'::bytea) RETURNING id::text`).
		Scan(&nasID); err != nil {
		t.Fatalf("insert nas: %v", err)
	}
	for _, s := range []struct {
		name string
		out  *string
	}{{"lobby", &svcA}, {"cafe", &svcB}} {
		if err := pool.QueryRow(ctx,
			`INSERT INTO nas_services (nas_id, service, ros_server_name, enabled)
			 VALUES ($1::uuid, 'hotspot', $2, true) RETURNING id::text`, nasID, s.name).Scan(s.out); err != nil {
			t.Fatalf("insert service %s: %v", s.name, err)
		}
	}

	type want struct {
		nasID, serviceID string
	}
	subs := []struct {
		username string
		nasID    *string
		svcID    *string
		want     []want // the scope rows expected after the migration
	}{
		{"scope_any", nil, nil, nil},
		{"scope_nas_wide", &nasID, nil, []want{{nasID, ""}}},
		{"scope_pinned", &nasID, &svcA, []want{{nasID, svcA}}},
	}
	for _, s := range subs {
		if _, err := pool.Exec(ctx,
			`INSERT INTO subscribers (username, password_enc, status, nas_id, nas_service_id)
			 VALUES ($1, '\x0102'::bytea, 'active', $2::uuid, $3::uuid)`,
			s.username, s.nasID, s.svcID); err != nil {
			t.Fatalf("insert %s: %v", s.username, err)
		}
	}

	// 3. Upgrade.
	m2 := migrator(t, dbURL)
	if err := m2.Migrate(headMigration); err != nil {
		t.Fatalf("migrate to %d: %v", headMigration, err)
	}
	if srcErr, dbErr := m2.Close(); srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	// 4a. Every pair became exactly its scope row — and the unscoped subscriber
	// got none, staying "any NAS" rather than being pinned to something.
	for _, s := range subs {
		rows, err := pool.Query(ctx,
			`SELECT sc.nas_id::text, COALESCE(sc.nas_service_id::text, '')
			   FROM subscriber_nas_scopes sc
			   JOIN subscribers s ON s.id = sc.subscriber_id
			  WHERE s.username = $1`, s.username)
		if err != nil {
			t.Fatal(err)
		}
		var got []want
		for rows.Next() {
			var w want
			if err := rows.Scan(&w.nasID, &w.serviceID); err != nil {
				t.Fatal(err)
			}
			got = append(got, w)
		}
		rows.Close()
		if len(got) != len(s.want) {
			t.Errorf("%s: %d scope rows after migration, want %d (%+v)", s.username, len(got), len(s.want), got)
			continue
		}
		for i := range got {
			if got[i] != s.want[i] {
				t.Errorf("%s: scope %d = %+v, want %+v", s.username, i, got[i], s.want[i])
			}
		}
	}

	// 4b. The pair columns are gone on both tables (the drop half of 0504), so
	// they cannot drift out of sync with the scope table.
	for _, tbl := range []string{"subscribers", "profiles"} {
		for _, col := range []string{"nas_id", "nas_service_id"} {
			var exists bool
			if err := pool.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM information_schema.columns
				                 WHERE table_name=$1 AND column_name=$2)`, tbl, col).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Errorf("%s.%s still exists after 0504", tbl, col)
			}
		}
	}

	// 4c. Deleting a service instance must degrade "only the Lobby zone" to
	// "this NAS", NOT to "any NAS". Dropping the row instead of SET NULL would
	// silently widen the account — the failure this whole feature prevents.
	if _, err := pool.Exec(ctx, `DELETE FROM nas_services WHERE id = $1::uuid`, svcA); err != nil {
		t.Fatalf("delete service: %v", err)
	}
	var nasIDAfter string
	var svcAfter *string
	if err := pool.QueryRow(ctx,
		`SELECT sc.nas_id::text, sc.nas_service_id::text
		   FROM subscriber_nas_scopes sc
		   JOIN subscribers s ON s.id = sc.subscriber_id
		  WHERE s.username = 'scope_pinned'`).Scan(&nasIDAfter, &svcAfter); err != nil {
		t.Fatalf("the pinned subscriber's scope vanished when its service was deleted: %v", err)
	}
	if nasIDAfter != nasID {
		t.Errorf("scope moved to NAS %s after the service was deleted, want %s", nasIDAfter, nasID)
	}
	if svcAfter != nil {
		t.Errorf("nas_service_id = %v after its service was deleted, want NULL (whole-NAS)", *svcAfter)
	}
	_ = svcB
}
