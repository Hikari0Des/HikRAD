package livestate

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFieldRoundTrip(t *testing.T) {
	nasID := "11111111-2222-3333-4444-555555555555"
	// Acct-Session-Id can itself contain a colon; the split must still be exact.
	acct := "81d0:00ff"
	f := Field(nasID, acct)
	gotNAS, gotAcct := ParseField(f)
	if gotNAS != nasID || gotAcct != acct {
		t.Fatalf("roundtrip: nas=%q acct=%q", gotNAS, gotAcct)
	}
}

func TestStateMarshalRoundTrip(t *testing.T) {
	s := State{
		Username:      "noor",
		SubscriberID:  "sub-1",
		NASID:         "nas-1",
		AcctSessionID: "sess-1",
		IP:            "10.0.0.5",
		MAC:           "AA:BB:CC:DD:EE:FF",
		StartedAt:     time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC),
		LastInterimAt: time.Date(2026, 7, 10, 10, 5, 0, 0, time.UTC),
		BytesIn:       123,
		BytesOut:      456,
		RateDownBps:   1000,
		RateUpBps:     500,
		Stale:         true,
		Service:       ServiceHotspot,
	}
	raw, err := s.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != s {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", got, s)
	}
}

func TestEventJSONShape(t *testing.T) {
	s := State{NASID: "n", AcctSessionID: "a", Service: ServicePPPoE}
	evt := Event{Op: OpUpsert, Field: Field("n", "a"), State: &s}
	raw, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	var back Event
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Op != OpUpsert || back.Field != "n:a" || back.State == nil || back.State.NASID != "n" {
		t.Fatalf("event decode mismatch: %+v", back)
	}
	// A remove event carries no state.
	rem := Event{Op: OpRemove, Field: "n:a"}
	raw, _ = json.Marshal(rem)
	var back2 Event
	_ = json.Unmarshal(raw, &back2)
	if back2.State != nil {
		t.Fatalf("remove should have nil state, got %+v", back2.State)
	}
}
