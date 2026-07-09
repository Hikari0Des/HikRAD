package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	cases := [][]string{
		{"9b48b36c-0488-4d3b-8bd9-e1f38755d965"},
		{"2026-07-08T10:00:00Z", "42"},
		{""},
		{"عربي", "kurdî"}, // cursors must survive non-ASCII keyset values
	}
	for _, want := range cases {
		got, err := DecodeCursor(EncodeCursor(want...))
		if err != nil {
			t.Fatalf("DecodeCursor(EncodeCursor(%q)): %v", want, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip = %q, want %q", got, want)
		}
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"not/base64!", "AAAA", "eyJ9"} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Fatalf("DecodeCursor(%q) should fail", bad)
		}
	}
}

func TestParsePage(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/subscribers", nil)
	p, err := ParsePage(r)
	if err != nil || p.Limit != DefaultLimit || p.Cursor != nil {
		t.Fatalf("defaults: %+v, err=%v", p, err)
	}

	r = httptest.NewRequest("GET", "/x?limit=500", nil)
	if p, _ = ParsePage(r); p.Limit != MaxLimit {
		t.Fatalf("limit should cap at %d, got %d", MaxLimit, p.Limit)
	}

	for _, bad := range []string{"limit=0", "limit=-1", "limit=abc"} {
		r = httptest.NewRequest("GET", "/x?"+bad, nil)
		if _, err := ParsePage(r); err == nil {
			t.Fatalf("ParsePage(%s) should fail", bad)
		}
	}

	r = httptest.NewRequest("GET", "/x?cursor="+EncodeCursor("abc"), nil)
	p, err = ParsePage(r)
	if err != nil || len(p.Cursor) != 1 || p.Cursor[0] != "abc" {
		t.Fatalf("cursor parse: %+v, err=%v", p, err)
	}

	r = httptest.NewRequest("GET", "/x?cursor=%21%21", nil)
	if _, err := ParsePage(r); err == nil {
		t.Fatal("invalid cursor should fail")
	}
}

func TestListResponseNextCursorNull(t *testing.T) {
	b, _ := json.Marshal(NewListResponse([]int{1, 2}, ""))
	if string(b) != `{"items":[1,2],"next_cursor":null}` {
		t.Fatalf("last page = %s", b)
	}
	b, _ = json.Marshal(NewListResponse([]int{1}, "abc"))
	if string(b) != `{"items":[1],"next_cursor":"abc"}` {
		t.Fatalf("page with next = %s", b)
	}
	// nil items must serialize as [], never null.
	b, _ = json.Marshal(NewListResponse[int](nil, ""))
	if string(b) != `{"items":[],"next_cursor":null}` {
		t.Fatalf("empty page = %s", b)
	}
}
