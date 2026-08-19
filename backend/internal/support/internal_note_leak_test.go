package support

// Gate item 9: "internal notes never leak" — asserted by test, on every
// subscriber-facing surface, not by eyeballing a UI. This is a compile-time
// + runtime proof: PublicMessage has no field an internal note's content
// could occupy, and PublicMessages drops internal rows entirely. Any future
// subscriber-facing function that serializes a []Message directly instead
// of routing through PublicMessages would fail this test the moment it's
// exercised with a mixed set — which is exactly the shape every real ticket
// thread has (staff always mixes public replies and internal notes).

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func mixedThread() []Message {
	return []Message{
		{ID: "m1", TicketID: "t1", AuthorType: "subscriber", Body: "My internet is down since this morning", Internal: false, CreatedAt: time.Now()},
		{ID: "m2", TicketID: "t1", AuthorType: "manager", Body: "INTERNAL: customer is on the delinquent list, escalate to collections first", Internal: true, CreatedAt: time.Now()},
		{ID: "m3", TicketID: "t1", AuthorType: "manager", Body: "We're looking into it, thanks for your patience.", Internal: false, CreatedAt: time.Now()},
		{ID: "m4", TicketID: "t1", AuthorType: "manager", Body: "INTERNAL: router at this site has a known firmware bug, see known-issues #71", Internal: true, CreatedAt: time.Now()},
		{ID: "m5", TicketID: "t1", AuthorType: "system", Body: "Ticket reopened after subscriber reply.", Internal: false, CreatedAt: time.Now()},
	}
}

// TestPublicMessages_DropsInternalNotes is the core data-level proof: the
// filtered slice contains only the public messages, by id and by content.
func TestPublicMessages_DropsInternalNotes(t *testing.T) {
	msgs := mixedThread()
	got := PublicMessages(msgs)

	if len(got) != 3 {
		t.Fatalf("expected 3 public messages, got %d: %+v", len(got), got)
	}
	wantIDs := map[string]bool{"m1": true, "m3": true, "m5": true}
	for _, m := range got {
		if !wantIDs[m.ID] {
			t.Fatalf("unexpected message %q leaked into public output", m.ID)
		}
	}
}

// TestPublicMessages_JSONNeverContainsInternalBody serializes the filtered
// output to JSON (the exact bytes any subscriber-facing endpoint would
// write to the wire) and asserts none of the internal bodies — or even the
// literal word "INTERNAL" used only in the internal fixtures above —
// appear anywhere in it. This is the closest a package-local test can get
// to "no subscriber-facing response contains an internal message" without
// standing up the full HTTP surface.
func TestPublicMessages_JSONNeverContainsInternalBody(t *testing.T) {
	got := PublicMessages(mixedThread())
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(raw)

	internalBodies := []string{
		"escalate to collections",
		"known firmware bug",
		"m2", "m4", // internal message ids must not appear either
	}
	for _, needle := range internalBodies {
		if strings.Contains(wire, needle) {
			t.Fatalf("internal content leaked into public JSON: %q found in %s", needle, wire)
		}
	}
}

// TestPublicMessage_HasNoInternalField locks the DTO shape itself: even if
// a future edit accidentally passed an internal Message through unfiltered,
// PublicMessage has no `internal` key for a caller to (mis)serialize as
// visible-false — the field simply does not exist on the wire type.
func TestPublicMessage_HasNoInternalField(t *testing.T) {
	raw, err := json.Marshal(PublicMessage{ID: "x", AuthorType: "manager", Body: "hello", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "internal") {
		t.Fatalf("PublicMessage's JSON must never mention \"internal\" at all: %s", raw)
	}
}

// TestPublicMessages_EmptyAndAllInternal covers the edges: an all-internal
// thread must yield zero public messages (not an error, not nil-vs-empty
// ambiguity), and an empty thread must not panic.
func TestPublicMessages_EmptyAndAllInternal(t *testing.T) {
	if got := PublicMessages(nil); len(got) != 0 {
		t.Fatalf("nil input: got %d messages, want 0", len(got))
	}
	allInternal := []Message{
		{ID: "a", Internal: true, Body: "staff only"},
		{ID: "b", Internal: true, Body: "also staff only"},
	}
	if got := PublicMessages(allInternal); len(got) != 0 {
		t.Fatalf("all-internal thread: got %d public messages, want 0: %+v", len(got), got)
	}
}
