package updates

// SSE framing, deliberately the same shape as internal/live/sse.go's
// (unexported there, so reimplemented here rather than imported) — this
// package's stream is a second, independent SSE feed, not a live/ consumer.

import (
	"encoding/json"
	"net/http"
)

func writeSSE(w http.ResponseWriter, flusher http.Flusher, evt string, data []byte) bool {
	if _, err := w.Write(encodeSSE(evt, data)); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeSSEComment(w http.ResponseWriter, flusher http.Flusher) bool {
	if _, err := w.Write([]byte(": ping\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func encodeSSE(evt string, data []byte) []byte {
	out := make([]byte, 0, len(evt)+len(data)+16)
	out = append(out, "event: "...)
	out = append(out, evt...)
	out = append(out, '\n')
	out = append(out, "data: "...)
	out = append(out, data...)
	out = append(out, '\n', '\n')
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
