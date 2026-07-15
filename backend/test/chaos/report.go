package main

import (
	"encoding/json"
	"os"
	"time"
)

// ScenarioResult is the JSON shape every scenario emits; docs/evidence's
// report generator folds these into the dated markdown report.
type ScenarioResult struct {
	Name      string          `json:"name"`
	Pass      bool            `json:"pass"`
	Detail    string          `json:"detail"`
	StartedAt time.Time       `json:"started_at"`
	Elapsed   time.Duration   `json:"elapsed_ns"`
	Flood     *FloodResult    `json:"flood,omitempty"`
	Counters  CounterSnapshot `json:"counters"`
	Extra     map[string]any  `json:"extra,omitempty"`
}

func writeReport(path string, r *ScenarioResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
