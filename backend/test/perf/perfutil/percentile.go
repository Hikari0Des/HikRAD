package perfutil

import (
	"sort"
	"time"
)

// Percentiles returns p50/p95/p99 (nearest-rank) over a set of latency
// samples. Callers pass a copy they're fine with being sorted in place.
type Percentiles struct {
	P50, P95, P99, Min, Max time.Duration
	N                       int
}

func ComputePercentiles(samples []time.Duration) Percentiles {
	if len(samples) == 0 {
		return Percentiles{}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	pick := func(p float64) time.Duration {
		idx := int(p*float64(len(samples))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		return samples[idx]
	}
	return Percentiles{
		P50: pick(0.50), P95: pick(0.95), P99: pick(0.99),
		Min: samples[0], Max: samples[len(samples)-1], N: len(samples),
	}
}
