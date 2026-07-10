package live

import "testing"

func TestEncodeSSE(t *testing.T) {
	got := string(encodeSSE("upsert", []byte(`{"a":1}`)))
	want := "event: upsert\ndata: {\"a\":1}\n\n"
	if got != want {
		t.Fatalf("encodeSSE:\n got %q\nwant %q", got, want)
	}
}

func TestEncodeSSESnapshot(t *testing.T) {
	got := string(encodeSSE("snapshot", []byte(`[]`)))
	want := "event: snapshot\ndata: []\n\n"
	if got != want {
		t.Fatalf("snapshot frame:\n got %q\nwant %q", got, want)
	}
}
