package updates

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEncodeSSE(t *testing.T) {
	got := string(encodeSSE("progress", []byte(`{"stage":"backup"}`)))
	want := "event: progress\ndata: {\"stage\":\"backup\"}\n\n"
	if got != want {
		t.Fatalf("encodeSSE:\n got %q\nwant %q", got, want)
	}
}

// TestSSERelayShape asserts the daemon-event-to-SSE-frame mapping C4
// specifies: a "stage" Event becomes an `event: progress` frame, and a
// "result" Event becomes `event: done` (ok:true) or `event: rolled_back`
// (ok:false) — using a real httptest.ResponseRecorder as the io.Writer/
// http.Flusher pair emitDaemonEvent expects, the same interface the real
// handler drives.
func TestSSERelayShape(t *testing.T) {
	rec := httptest.NewRecorder()
	flusher, ok := http.ResponseWriter(rec).(http.Flusher)
	if !ok {
		t.Fatal("httptest.ResponseRecorder does not implement http.Flusher")
	}

	stage := event{Type: "stage", Stage: "backup", TS: "2026-07-18T12:00:00Z"}
	ok1 := true
	success := event{Type: "result", OK: &ok1, Version: "v2.7.0", Message: "Update complete and healthy."}
	ok2 := false
	failure := event{Type: "result", OK: &ok2, Version: "v2.6.0", Message: "rolled back"}

	if !emitDaemonEvent(rec, flusher, stage) {
		t.Fatal("emitDaemonEvent(stage) returned false")
	}
	if !emitDaemonEvent(rec, flusher, success) {
		t.Fatal("emitDaemonEvent(success result) returned false")
	}
	if !emitDaemonEvent(rec, flusher, failure) {
		t.Fatal("emitDaemonEvent(failure result) returned false")
	}

	body := rec.Body.String()
	frames := bytes.Split([]byte(body), []byte("\n\n"))
	if len(frames) < 3 {
		t.Fatalf("expected at least 3 SSE frames, got %d: %q", len(frames), body)
	}

	assertFrame(t, string(frames[0]), "progress", func(data map[string]any) {
		if data["stage"] != "backup" {
			t.Errorf("progress frame stage = %v, want backup", data["stage"])
		}
	})
	assertFrame(t, string(frames[1]), "done", func(data map[string]any) {
		if data["version"] != "v2.7.0" {
			t.Errorf("done frame version = %v, want v2.7.0", data["version"])
		}
	})
	assertFrame(t, string(frames[2]), "rolled_back", func(data map[string]any) {
		if data["version"] != "v2.6.0" {
			t.Errorf("rolled_back frame version = %v, want v2.6.0", data["version"])
		}
	})
}

func assertFrame(t *testing.T, frame, wantEvent string, checkData func(map[string]any)) {
	t.Helper()
	var gotEvent, dataLine string
	for _, line := range strings.Split(frame, "\n") {
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			gotEvent = after
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			dataLine = after
		}
	}
	if gotEvent != wantEvent {
		t.Fatalf("frame event = %q, want %q (frame: %q)", gotEvent, wantEvent, frame)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(dataLine), &data); err != nil {
		t.Fatalf("unmarshal data line %q: %v", dataLine, err)
	}
	checkData(data)
}
