// Command resources is the NFR-3 headroom sampler: polls `docker stats` for
// the given container names for a duration (run this alongside a chaos/perf
// load phase) and reports peak CPU%/memory per container, so the evidence
// report can show real headroom against the 4 vCPU / 8 GB tier.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type sample struct {
	CPUPercent float64
	MemBytes   float64
}

func main() { os.Exit(run()) }

func run() int {
	names := flag.String("containers", "", "comma-separated docker container names to sample")
	duration := flag.Duration("duration", 30*time.Second, "sampling duration")
	interval := flag.Duration("interval", 2*time.Second, "sampling interval")
	out := flag.String("out", "../docs/evidence/raw/resources.json", "JSON report path (relative to backend/)")
	flag.Parse()

	targets := strings.Split(*names, ",")
	peaks := map[string]*sample{}
	for _, n := range targets {
		n = strings.TrimSpace(n)
		if n != "" {
			peaks[n] = &sample{}
		}
	}
	if len(peaks) == 0 {
		fmt.Fprintln(os.Stderr, "-containers is required")
		return 2
	}

	deadline := time.Now().Add(*duration)
	for time.Now().Before(deadline) {
		for name := range peaks {
			cpu, mem, err := statOnce(name)
			if err != nil {
				continue
			}
			if cpu > peaks[name].CPUPercent {
				peaks[name].CPUPercent = cpu
			}
			if mem > peaks[name].MemBytes {
				peaks[name].MemBytes = mem
			}
		}
		time.Sleep(*interval)
	}

	rep := map[string]any{"peaks": peaks, "vcpu_budget": 4, "mem_budget_gb": 8, "generated_at": time.Now().UTC()}
	fmt.Printf("%+v\n", peaks)
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err == nil {
		if f, err := os.Create(*out); err == nil {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rep)
			f.Close()
		}
	}
	return 0
}

// statOnce parses `docker stats --no-stream` for one container: CPU% and
// memory usage (bytes, the "used" half of "used / limit").
func statOnce(name string) (cpuPct, memBytes float64, err error) {
	cmd := exec.Command("docker", "stats", "--no-stream", "--format", "{{.CPUPerc}}\t{{.MemUsage}}", name)
	outBytes, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(outBytes)))
	if !sc.Scan() {
		return 0, 0, fmt.Errorf("no stats line for %s", name)
	}
	parts := strings.SplitN(sc.Text(), "\t", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected stats format: %q", sc.Text())
	}
	cpuPct, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[0]), "%"), 64)
	memBytes = parseMemUsage(parts[1])
	return cpuPct, memBytes, nil
}

// parseMemUsage parses docker's "123.4MiB / 1GiB" format, returning the used
// side in bytes.
func parseMemUsage(s string) float64 {
	used := strings.TrimSpace(strings.SplitN(s, "/", 2)[0])
	var num float64
	var unit string
	for i, r := range used {
		if (r < '0' || r > '9') && r != '.' {
			num, _ = strconv.ParseFloat(used[:i], 64)
			unit = used[i:]
			break
		}
	}
	mult := 1.0
	switch strings.TrimSpace(unit) {
	case "KiB":
		mult = 1024
	case "MiB":
		mult = 1024 * 1024
	case "GiB":
		mult = 1024 * 1024 * 1024
	case "B", "":
		mult = 1
	}
	return num * mult
}
