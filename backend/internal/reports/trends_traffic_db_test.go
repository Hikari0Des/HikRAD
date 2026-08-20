package reports_test

// FR-173 DB-backed suite: trend lines, daily traffic, top-10s, and traffic
// by entity — all read real usage_points/usage_card_daily/subscribers data,
// against a real Postgres+TimescaleDB (gated on HIKRAD_TEST_DB_URL, same as
// db_test.go). Migrations 0633/0634 (usage_card_daily + its policy) are
// applied by setup(t)'s platform.Migrate call, same as every other migration.
//
// usage_daily/usage_card_daily bucket in UTC (verified against a live
// instance while building this suite — see trends.go's top10Handler doc
// comment): every fixture below uses explicit UTC timestamps well clear of
// a day boundary, so these tests are not racy against wall-clock time the
// way a bare "now()" fixture would be.

import (
	"context"
	"testing"
	"time"
)

func (e env) insertUsagePoint(t *testing.T, at time.Time, subscriberID, cardID string, down, up int64) {
	t.Helper()
	var subArg, cardArg any
	if subscriberID != "" {
		subArg = subscriberID
	}
	if cardID != "" {
		cardArg = cardID
	}
	if _, err := e.db.Exec(context.Background(),
		`INSERT INTO usage_points (time, subscriber_id, card_id, nas_id, delta_down, delta_up, service)
		 VALUES ($1, $2::uuid, $3::uuid, gen_random_uuid(), $4, $5, 'pppoe')`,
		at, subArg, cardArg, down, up); err != nil {
		t.Fatalf("insert usage_points: %v", err)
	}
}

func (e env) createCardGroup(t *testing.T) string {
	t.Helper()
	var id string
	if err := e.db.QueryRow(context.Background(),
		`INSERT INTO card_groups (name) VALUES ($1) RETURNING id::text`, uniq("group_")).Scan(&id); err != nil {
		t.Fatalf("seed card group: %v", err)
	}
	return id
}

func (e env) createCard(t *testing.T, groupID string) string {
	t.Helper()
	var id string
	if err := e.db.QueryRow(context.Background(),
		`INSERT INTO cards (group_id, code_hash) VALUES ($1::uuid, $2) RETURNING id::text`,
		groupID, uniq("hash_")).Scan(&id); err != nil {
		t.Fatalf("seed card: %v", err)
	}
	return id
}

// TestDailyTraffic_MatchesUsagePointsSum: three days of controlled traffic
// for two subscribers; the report's per-day sums must equal a direct SQL sum
// over the same instant range — not sampled, not eyeballed.
func TestDailyTraffic_MatchesUsagePointsSum(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 10_000, 30)
	subA := e.createSubscriber(t, prof)
	subB := e.createSubscriber(t, prof)

	// A randomized 3-day window (like uniq() elsewhere in this package's
	// tests) so a repeated run against this shared, never-reset test DB
	// doesn't double up on a fixed date's totals — safely inside UTC day
	// boundaries (noon UTC, never near midnight), so the aggregate's day
	// bucket is unambiguous.
	day0 := time.Date(1990+int(randInt(t, 30)), time.Month(1+randInt(t, 12)), 1+int(randInt(t, 27)), 12, 0, 0, 0, time.UTC)
	day1 := day0.AddDate(0, 0, 1)
	day2 := day0.AddDate(0, 0, 2)

	e.insertUsagePoint(t, day0, subA, "", 1000, 200)
	e.insertUsagePoint(t, day0, subB, "", 500, 100)
	e.insertUsagePoint(t, day1, subA, "", 300, 50)
	e.insertUsagePoint(t, day2, subB, "", 700, 70)

	from := day0.AddDate(0, 0, -1)
	to := day2.AddDate(0, 0, 1)
	q := "?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	r := e.do(t, "GET", "/api/v1/reports/daily-traffic"+q, e.token, nil)
	if r.status != 200 {
		t.Fatalf("daily-traffic = %d: %s", r.status, r.body)
	}
	var out struct {
		Rows []struct {
			Date      string `json:"date"`
			DownBytes int64  `json:"down_bytes"`
			UpBytes   int64  `json:"up_bytes"`
		} `json:"rows"`
	}
	r.into(t, &out)

	want := map[string][2]int64{
		day0.Format("2006-01-02"): {1500, 300},
		day1.Format("2006-01-02"): {300, 50},
		day2.Format("2006-01-02"): {700, 70},
	}
	got := map[string][2]int64{}
	for _, row := range out.Rows {
		got[row.Date] = [2]int64{row.DownBytes, row.UpBytes}
	}
	for date, w := range want {
		g, ok := got[date]
		if !ok {
			t.Fatalf("missing day %s in response: %+v", date, out.Rows)
		}
		if g != w {
			t.Fatalf("day %s: got down=%d up=%d, want down=%d up=%d", date, g[0], g[1], w[0], w[1])
		}
	}
}

// TestTrafficByEntity_UsersAndCardsNeverMix — phase gate item 2, proven
// directly: a subscriber's traffic must never appear in the Cards tab, and a
// card group's traffic must never appear in the Users tab, even though both
// exist in the same window.
func TestTrafficByEntity_UsersAndCardsNeverMix(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 10_000, 30)
	sub := e.createSubscriber(t, prof)
	group := e.createCardGroup(t)
	card := e.createCard(t, group)

	now := time.Now().UTC()
	// Comfortably inside "today" and "this month" regardless of the exact
	// instant the test runs, and far from midnight in either UTC or Baghdad.
	at := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.UTC)
	if now.Hour() < 4 { // near-midnight guard: fall back to yesterday's noon-UTC point still inside "this month" for the rare unlucky run
		at = at.AddDate(0, 0, -1)
	}
	e.insertUsagePoint(t, at, sub, "", 42_000_000, 8_000_000)

	// The Cards-tab fixture needs an actual card row for the join, but the
	// continuous aggregate only cares about card_id — insert it directly
	// tagged to the seeded card.
	if _, err := e.db.Exec(context.Background(),
		`INSERT INTO usage_points (time, card_id, nas_id, delta_down, delta_up, service)
		 VALUES ($1, $2::uuid, gen_random_uuid(), $3, $4, 'hotspot')`,
		at, card, int64(9_000_000), int64(1_000_000)); err != nil {
		t.Fatalf("insert card usage: %v", err)
	}

	users := e.do(t, "GET", "/api/v1/reports/traffic-by-entity?view=users&limit=50", e.token, nil)
	if users.status != 200 {
		t.Fatalf("traffic-by-entity users = %d: %s", users.status, users.body)
	}
	var uOut struct {
		Items []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			MonthDownB int64  `json:"month_down_bytes"`
			MonthUpB   int64  `json:"month_up_bytes"`
		} `json:"items"`
	}
	users.into(t, &uOut)
	foundSub, foundGroupInUsers := false, false
	for _, it := range uOut.Items {
		if it.ID == sub {
			foundSub = true
			if it.MonthDownB != 42_000_000 || it.MonthUpB != 8_000_000 {
				t.Fatalf("users tab subscriber bytes = %d/%d, want 42000000/8000000", it.MonthDownB, it.MonthUpB)
			}
		}
		if it.ID == group {
			foundGroupInUsers = true
		}
	}
	if !foundSub {
		t.Fatalf("users tab missing seeded subscriber: %+v", uOut.Items)
	}
	if foundGroupInUsers {
		t.Fatal("users tab leaked a card-group id — Users and Cards must never share a query")
	}

	cardsResp := e.do(t, "GET", "/api/v1/reports/traffic-by-entity?view=cards&limit=50", e.token, nil)
	if cardsResp.status != 200 {
		t.Fatalf("traffic-by-entity cards = %d: %s", cardsResp.status, cardsResp.body)
	}
	var cOut struct {
		Items []struct {
			ID         string `json:"id"`
			MonthDownB int64  `json:"month_down_bytes"`
			MonthUpB   int64  `json:"month_up_bytes"`
		} `json:"items"`
	}
	cardsResp.into(t, &cOut)
	foundGroup, foundSubInCards := false, false
	for _, it := range cOut.Items {
		if it.ID == group {
			foundGroup = true
			if it.MonthDownB != 9_000_000 || it.MonthUpB != 1_000_000 {
				t.Fatalf("cards tab group bytes = %d/%d, want 9000000/1000000", it.MonthDownB, it.MonthUpB)
			}
		}
		if it.ID == sub {
			foundSubInCards = true
		}
	}
	if !foundGroup {
		t.Fatalf("cards tab missing seeded card group: %+v", cOut.Items)
	}
	if foundSubInCards {
		t.Fatal("cards tab leaked a subscriber id — Users and Cards must never share a query")
	}
}

// TestTop10_RanksDescendingByBytes: three subscribers with distinct known
// download totals today; the top-10 list must rank them highest-first with
// the exact byte counts, not just "some order." Scoped to a fresh agent
// (owner_manager_id) rather than the unscoped admin view — top10Handler's
// leaderboard is genuinely global across ALL subscribers for an unscoped
// caller, so an unscoped assertion would collide with every other
// subscriber's "today" traffic already sitting in this shared, never-reset
// test DB (found while building this suite: a rerun without a DB reset
// pushed this test's own fixture out of the top 10 entirely). Scoping is
// the same isolation mechanism TestSettlementAndRevenueScopedManagerIsolation
// already uses for exactly this class of problem.
func TestTop10_RanksDescendingByBytes(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 10_000, 30)
	agentID, agentToken := e.seedAgent(t)
	low := e.createSubscriberOwned(t, prof, agentID)
	mid := e.createSubscriberOwned(t, prof, agentID)
	high := e.createSubscriberOwned(t, prof, agentID)

	now := time.Now().UTC()
	at := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
	if now.Hour() < 2 || now.Hour() > 22 {
		t.Skip("too close to a UTC day boundary for a deterministic 'today' fixture")
	}
	e.insertUsagePoint(t, at, low, "", 1_000_000, 0)
	e.insertUsagePoint(t, at, mid, "", 5_000_000, 0)
	e.insertUsagePoint(t, at, high, "", 20_000_000, 0)

	r := e.do(t, "GET", "/api/v1/reports/top10", agentToken, nil)
	if r.status != 200 {
		t.Fatalf("top10 = %d: %s", r.status, r.body)
	}
	var out struct {
		DownloadToday []struct {
			SubscriberID string `json:"subscriber_id"`
			Bytes        int64  `json:"bytes"`
		} `json:"download_today"`
	}
	r.into(t, &out)

	byID := map[string]int64{}
	for _, row := range out.DownloadToday {
		byID[row.SubscriberID] = row.Bytes
	}
	if byID[high] != 20_000_000 || byID[mid] != 5_000_000 || byID[low] != 1_000_000 {
		t.Fatalf("top10 download_today bytes wrong: high=%d mid=%d low=%d (rows=%+v)",
			byID[high], byID[mid], byID[low], out.DownloadToday)
	}
	// Descending order: high must appear before mid, mid before low.
	pos := map[string]int{}
	for i, row := range out.DownloadToday {
		pos[row.SubscriberID] = i
	}
	if !(pos[high] < pos[mid] && pos[mid] < pos[low]) {
		t.Fatalf("top10 download_today not descending: positions high=%d mid=%d low=%d", pos[high], pos[mid], pos[low])
	}
}

// TestTrendsReport_ActiveExpiredCounts: subscribers with controlled
// created_at/expires_at, asserted against a manually-computed expectation
// for each day in a fixed window — proves the per-day FILTER counts derive
// correctly from raw subscriber timestamps, not just "doesn't crash."
//
// Scoped to a fresh agent, same isolation reasoning as
// TestTop10_RanksDescendingByBytes: an unscoped read counts EVERY
// subscriber in the whole (shared, never-reset) test DB whose created_at/
// expires_at falls inside the random window, including backdated fixtures
// left behind by earlier runs of this very test — found while building this
// suite, where a randomized-but-unscoped version passed alone but failed
// intermittently when run alongside its own accumulated history.
func TestTrendsReport_ActiveExpiredCounts(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 10_000, 30)
	agentID, agentToken := e.seedAgent(t)

	// A randomized 6-day window (like uniq() elsewhere in this package's
	// tests), far from "now", so a rerun can't double-count a fixed
	// historical date.
	d0 := time.Date(1980+int(randInt(t, 40)), time.Month(1+randInt(t, 12)), 1+int(randInt(t, 20)), 8, 0, 0, 0, time.UTC)
	winFrom := time.Date(d0.Year(), d0.Month(), d0.Day(), 0, 0, 0, 0, time.UTC)
	winTo := winFrom.AddDate(0, 0, 5)

	// s1: created day 0, never expires -> active every day in the window.
	s1 := e.createSubscriberOwned(t, prof, agentID)
	// s2: created day 1, expires day 3 -> the handler's own convention (see
	// trends.go) counts the expiry day itself as still active (transition
	// happens the day AFTER expires_at's calendar date), so s2 is active
	// days 1-3 and expired from day 4.
	s2 := e.createSubscriberOwned(t, prof, agentID)
	ctx := context.Background()
	if _, err := e.db.Exec(ctx, `UPDATE subscribers SET created_at = $1, expires_at = NULL WHERE id = $2::uuid`,
		d0, s1); err != nil {
		t.Fatalf("update s1: %v", err)
	}
	if _, err := e.db.Exec(ctx, `UPDATE subscribers SET created_at = $1, expires_at = $2 WHERE id = $3::uuid`,
		d0.AddDate(0, 0, 1), d0.AddDate(0, 0, 3), s2); err != nil {
		t.Fatalf("update s2: %v", err)
	}

	q := "?from=" + winFrom.Format(time.RFC3339) + "&to=" + winTo.Format(time.RFC3339)
	r := e.do(t, "GET", "/api/v1/reports/trends"+q, agentToken, nil)
	if r.status != 200 {
		t.Fatalf("trends = %d: %s", r.status, r.body)
	}
	var out struct {
		Rows []struct {
			Date    string `json:"date"`
			Active  int64  `json:"active_subscribers"`
			Expired int64  `json:"expired_subscribers"`
		} `json:"rows"`
	}
	r.into(t, &out)

	byDate := map[string]struct{ active, expired int64 }{}
	for _, row := range out.Rows {
		byDate[row.Date] = struct{ active, expired int64 }{row.Active, row.Expired}
	}

	fmtDay := func(n int) string { return d0.AddDate(0, 0, n).Format("2006-01-02") }
	cases := []struct {
		date           string
		minActive      int64 // >= (a randomized window, but still guards against under-counting)
		exactlyExpired int64
	}{
		{fmtDay(0), 1, 0}, // s1 only
		{fmtDay(1), 2, 0}, // s1 + s2
		{fmtDay(2), 2, 0}, // s1 + s2
		{fmtDay(3), 2, 0}, // s1 + s2, expiry-day-itself still counts active
		{fmtDay(4), 1, 1}, // s1 active, s2 now expired
	}
	for _, c := range cases {
		got, ok := byDate[c.date]
		if !ok {
			t.Fatalf("missing date %s in trends response: %+v", c.date, out.Rows)
		}
		if got.active < c.minActive {
			t.Fatalf("date %s: active=%d, want >= %d", c.date, got.active, c.minActive)
		}
		if got.expired != c.exactlyExpired {
			t.Fatalf("date %s: expired=%d, want exactly %d", c.date, got.expired, c.exactlyExpired)
		}
	}
}
