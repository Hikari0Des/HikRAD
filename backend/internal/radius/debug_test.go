package radius

import (
	"encoding/json"
	"testing"
)

func mustDecisionJSON(t *testing.T, ev decisionEvent) string {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFilterDecision_NoFilters_MatchesAndMaps(t *testing.T) {
	raw := mustDecisionJSON(t, decisionEvent{
		Username: "ali", NASIP: "10.0.0.2", Outcome: "reject",
		Reason: ReasonBadPassword, Checks: []string{"nas", "user"}, At: "2026-07-11T00:00:00Z",
	})
	out, ok := filterDecision(raw, "", "")
	if !ok {
		t.Fatal("expected match with no filters")
	}
	var de debugEvent
	if err := json.Unmarshal(out, &de); err != nil {
		t.Fatal(err)
	}
	if de.NAS != "10.0.0.2" || de.Username != "ali" || de.Reason != "bad_password" {
		t.Errorf("mapped event = %+v", de)
	}
	if len(de.Checks) != 2 {
		t.Errorf("checks = %v", de.Checks)
	}
}

func TestFilterDecision_UsernameCaseInsensitive(t *testing.T) {
	raw := mustDecisionJSON(t, decisionEvent{Username: "Ali", NASIP: "10.0.0.2", Outcome: "accept"})
	if _, ok := filterDecision(raw, "ali", ""); !ok {
		t.Error("expected case-insensitive username match")
	}
	if _, ok := filterDecision(raw, "omar", ""); ok {
		t.Error("expected non-match for different username")
	}
}

func TestFilterDecision_NASFilterByIP(t *testing.T) {
	raw := mustDecisionJSON(t, decisionEvent{Username: "ali", NASIP: "10.0.0.2", Outcome: "accept"})
	if _, ok := filterDecision(raw, "", "10.0.0.2"); !ok {
		t.Error("expected NAS IP match")
	}
	if _, ok := filterDecision(raw, "", "10.0.0.9"); ok {
		t.Error("expected non-match for different NAS IP")
	}
	// Unknown-NAS sentinel matches nothing.
	if _, ok := filterDecision(raw, "", "\x00no-such-nas"); ok {
		t.Error("sentinel must never match")
	}
}

func TestFilterDecision_BadInput(t *testing.T) {
	if _, ok := filterDecision("", "", ""); ok {
		t.Error("empty raw must not match")
	}
	if _, ok := filterDecision("{not json", "", ""); ok {
		t.Error("malformed raw must not match")
	}
}

func TestFilterDecision_NilChecksBecomesEmptyArray(t *testing.T) {
	raw := mustDecisionJSON(t, decisionEvent{Username: "ali", NASIP: "10.0.0.2", Outcome: "accept"})
	out, ok := filterDecision(raw, "", "")
	if !ok {
		t.Fatal("expected match")
	}
	// checks must serialize as [] not null so the UI can render an empty trace.
	if !contains(out, `"checks":[]`) {
		t.Errorf("expected empty checks array, got %s", out)
	}
}

func contains(b []byte, sub string) bool {
	return len(b) >= len(sub) && indexOf(string(b), sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
