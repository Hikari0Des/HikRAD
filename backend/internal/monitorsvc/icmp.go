package monitorsvc

// ICMP reachability probe. Raw ICMP sockets need CAP_NET_RAW / privilege that we
// don't want the container to hold, and adding an ICMP library would pull a new
// dependency into an offline build. So production reachability shells out to the
// system `ping` (present in the monitor image), parsed for the round-trip time.
// The engine talks to it through the Pinger interface, so the state machine and
// engine tests inject a deterministic fake and never touch the network.

import (
	"bufio"
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// PingResult is one ICMP echo attempt.
type PingResult struct {
	OK        bool
	LatencyMS float64
}

// Pinger sends a single ICMP echo to ip and reports reachability + RTT. It must
// respect ctx (the engine bounds every probe with a timeout) and must never
// panic on an unreachable host — an unreachable host is OK=false, not an error.
type Pinger interface {
	Ping(ctx context.Context, ip string) PingResult
}

// systemPinger runs the OS ping binary once per call. Cross-platform flag/parse
// handling keeps it usable on the Linux container and a Windows dev box alike.
type systemPinger struct{}

// NewSystemPinger returns the production Pinger.
func NewSystemPinger() Pinger { return systemPinger{} }

var pingRTT = regexp.MustCompile(`time[=<]\s*([0-9]+(?:\.[0-9]+)?)`)

func (systemPinger) Ping(ctx context.Context, ip string) PingResult {
	// One echo, short deadline; the engine's ctx timeout is the hard stop so a
	// black-holed host can't wedge the worker.
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", "2000", ip)
	} else {
		// -c1 one packet, -w2 overall deadline (busybox/iputils both accept -w).
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-w", "2", ip)
	}
	out, err := cmd.Output()
	if err != nil {
		// Non-zero exit = unreachable/timeout. Not a probe error — a DOWN sample.
		return PingResult{OK: false}
	}
	// Parse the first "time=..ms" from the reply line.
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if m := pingRTT.FindStringSubmatch(sc.Text()); m != nil {
			if ms, perr := strconv.ParseFloat(m[1], 64); perr == nil {
				return PingResult{OK: true, LatencyMS: ms}
			}
		}
	}
	// Reachable but RTT unparsed (locale/format): still up, latency unknown.
	return PingResult{OK: true, LatencyMS: 0}
}

// pingTimeout bounds a single ICMP probe so a stuck host can't outlive one
// scheduler tick (edge case: "probe worker must not pile up when a NAS times
// out"). Kept below the 15 s ICMP interval with headroom.
const pingTimeout = 3 * time.Second
