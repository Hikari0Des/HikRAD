package livestate

// Real-Redis suite (gated on HIKRAD_TEST_REDIS_URL, same convention every
// other DB/Redis-gated test in this repo uses) for the v4-11 IP index —
// FR-167 condition 4 ("the IP matches EXACTLY ONE live session") depends
// entirely on ByIP/Upsert/Remove agreeing with each other, so this is the
// one piece of that phase's mechanism that gets proven against a real Redis
// rather than only reviewed, whenever one is reachable.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func redisForTest(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if url == "" {
		t.Skip("HIKRAD_TEST_REDIS_URL not set; skipping live-Redis IP index suite")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis at %s not reachable: %v", url, err)
	}
	return rdb
}

func uniq(t *testing.T, prefix string) string {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// TestByIP_ExactlyOneVsZeroVsAmbiguous is the direct, real-Redis proof of
// FR-167 condition 4's three outcomes: 0 sessions, 1 session, 2+ sessions on
// the same framed IP — the exact distinction portalapi's autologin.go bases
// its whole grant/refuse decision on.
func TestByIP_ExactlyOneVsZeroVsAmbiguous(t *testing.T) {
	rdb := redisForTest(t)
	ctx := context.Background()
	ip := "203.0.113." + uniq(t, "")[:2] // best-effort collision avoidance across parallel CI shards
	nas1, acct1, sub1 := uniq(t, "nas_"), uniq(t, "acct_"), uniq(t, "sub_")
	nas2, acct2, sub2 := uniq(t, "nas_"), uniq(t, "acct_"), uniq(t, "sub_")
	t.Cleanup(func() {
		_ = Remove(ctx, rdb, nas1, acct1, sub1, ServicePPPoE)
		_ = Remove(ctx, rdb, nas2, acct2, sub2, ServicePPPoE)
	})

	// Zero live sessions on this IP.
	got, err := ByIP(ctx, rdb, ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ByIP on an unused IP = %d sessions, want 0", len(got))
	}

	// Exactly one.
	s1 := State{NASID: nas1, AcctSessionID: acct1, SubscriberID: sub1, IP: ip, MAC: "AA:BB:CC:DD:EE:01",
		Service: ServicePPPoE, StartedAt: time.Now(), LastInterimAt: time.Now()}
	if err := Upsert(ctx, rdb, s1); err != nil {
		t.Fatal(err)
	}
	got, err = ByIP(ctx, rdb, ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SubscriberID != sub1 {
		t.Fatalf("ByIP after one Upsert = %+v, want exactly [sub=%s]", got, sub1)
	}

	// A second, DIFFERENT subscriber's session lands on the same IP —
	// exactly the CGNAT/shared-NAT scenario the phase brief names as the
	// normal case in this market. ByIP must now report 2, unambiguously.
	s2 := State{NASID: nas2, AcctSessionID: acct2, SubscriberID: sub2, IP: ip, MAC: "AA:BB:CC:DD:EE:02",
		Service: ServicePPPoE, StartedAt: time.Now(), LastInterimAt: time.Now()}
	if err := Upsert(ctx, rdb, s2); err != nil {
		t.Fatal(err)
	}
	got, err = ByIP(ctx, rdb, ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ByIP with two subscribers sharing one IP = %d, want 2 (ambiguous — this is what a refusal is built on)", len(got))
	}

	// Removing one drops the index back to exactly one, and Remove's own
	// IP-cleanup (it reads the hash to learn the IP before deleting, since
	// none of its existing callers carry the IP) must have removed s1's
	// membership specifically, not s2's.
	if err := Remove(ctx, rdb, nas1, acct1, sub1, ServicePPPoE); err != nil {
		t.Fatal(err)
	}
	got, err = ByIP(ctx, rdb, ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SubscriberID != sub2 {
		t.Fatalf("ByIP after removing s1 = %+v, want exactly [sub=%s]", got, sub2)
	}

	// Removing the last one empties the index.
	if err := Remove(ctx, rdb, nas2, acct2, sub2, ServicePPPoE); err != nil {
		t.Fatal(err)
	}
	got, err = ByIP(ctx, rdb, ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ByIP after removing every session = %d, want 0 (index must not leak stale membership)", len(got))
	}
}

// TestByIP_StaleIndexMembershipNeverInflatesCount guards the defensive check
// in ByIP (only entries whose stored State.IP still matches count) against
// the one race the doc comment calls out: a field present in the live:ip:*
// set whose hash entry has since moved to a different IP (re-upserted with a
// new framed IP by a later accounting packet) must not be double-counted
// under the OLD IP.
func TestByIP_StaleIndexMembershipNeverInflatesCount(t *testing.T) {
	rdb := redisForTest(t)
	ctx := context.Background()
	oldIP := "203.0.113." + uniq(t, "")[:2]
	newIP := "203.0.113." + uniq(t, "")[:2]
	nas, acct, sub := uniq(t, "nas_"), uniq(t, "acct_"), uniq(t, "sub_")
	t.Cleanup(func() { _ = Remove(ctx, rdb, nas, acct, sub, ServicePPPoE) })

	if err := Upsert(ctx, rdb, State{NASID: nas, AcctSessionID: acct, SubscriberID: sub, IP: oldIP,
		Service: ServicePPPoE, StartedAt: time.Now(), LastInterimAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// Simulate a roaming/renumbered session: re-Upsert the SAME field with a
	// new IP. The hash now reflects newIP; live:ip:<oldIP> may still hold a
	// stale membership until something touches it (Upsert only ADDS to the
	// new IP's set, by design — it does not know the previous value without
	// a read, same tradeoff Remove resolves by reading first).
	if err := Upsert(ctx, rdb, State{NASID: nas, AcctSessionID: acct, SubscriberID: sub, IP: newIP,
		Service: ServicePPPoE, StartedAt: time.Now(), LastInterimAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	gotOld, err := ByIP(ctx, rdb, oldIP)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotOld) != 0 {
		t.Fatalf("ByIP(oldIP) after the session moved = %d, want 0 (stale index membership must not count)", len(gotOld))
	}
	gotNew, err := ByIP(ctx, rdb, newIP)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotNew) != 1 || gotNew[0].SubscriberID != sub {
		t.Fatalf("ByIP(newIP) = %+v, want exactly [sub=%s]", gotNew, sub)
	}
}
