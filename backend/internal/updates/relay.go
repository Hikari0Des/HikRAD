package updates

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"
)

// relayRun mirrors hikrad-updaterd's own *run (backend/cmd/hikrad-updaterd/
// run.go): the POST handler that started an update keeps reading the
// daemon's stream in the background regardless of whether anyone is
// watching, and the SSE handler tails the buffer this accumulates. This is
// what lets a SEPARATE GET /system/update/stream connection (a page reload,
// or the panel's very first request after its own container comes back)
// observe an update that a DIFFERENT HTTP request originally started.
//
// It does NOT survive a hikrad-api process restart — if the panel's own
// container is replaced mid-update (the case this whole feature exists
// for), the new process has no relayRun at all, and the SSE handler falls
// back to polling the daemon's status/rollback-status verbs instead (C4) —
// still correct, just less granular (no per-line stage timestamps from
// before the restart), matching FR-87.2's "reconnect/poll fallback."
type relayRun struct {
	mu     sync.Mutex
	events []event
	doneCh chan struct{}
}

var (
	currentMu sync.Mutex
	current   *relayRun
)

func (rr *relayRun) append(e event) {
	rr.mu.Lock()
	rr.events = append(rr.events, e)
	rr.mu.Unlock()
}

// pump reads the daemon's stream lines (already past the first line, which
// the POST handler consumed itself to decide 202-vs-error) until the
// connection ends, then closes doneCh. Detached from any HTTP request's
// lifetime, same reasoning as the daemon's own runUpdate.
func (rr *relayRun) pump(conn net.Conn, r *bufio.Reader) {
	defer func() {
		_ = conn.Close()
		close(rr.doneCh)
		currentMu.Lock()
		if current == rr {
			current = nil
		}
		currentMu.Unlock()
	}()
	for {
		line, err := r.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			var e event
			if json.Unmarshal([]byte(trimmed), &e) == nil {
				rr.append(e)
				if e.Type == "result" {
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// tail streams every event from the start, then polls for new ones until
// doneCh closes — same shape as the daemon's own run.tail, translated into
// SSE frames by the caller.
func (rr *relayRun) tail(emit func(event) bool) {
	idx := 0
	for {
		rr.mu.Lock()
		pending := append([]event(nil), rr.events[idx:]...)
		idx = len(rr.events)
		rr.mu.Unlock()
		for _, e := range pending {
			if !emit(e) {
				return
			}
		}
		select {
		case <-rr.doneCh:
			rr.mu.Lock()
			rest := append([]event(nil), rr.events[idx:]...)
			rr.mu.Unlock()
			for _, e := range rest {
				emit(e)
			}
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}
