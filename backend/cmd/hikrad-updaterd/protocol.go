package main

import (
	"encoding/json"
	"io"
)

// Socket protocol (C2, docs/v2/phases/phase-v2-7-one-click-update/00-phase.md):
// newline-delimited JSON, one request per line, one connection per request.

// Request is the only shape ever accepted on the socket. Requester is not
// part of the frozen wire contract's example payload but is needed to fill
// in C2's own documented lock_owner example ("panel:<manager-email>") — set
// by hikrad-api's relay, defaults to "socket" when absent.
type Request struct {
	Verb       string `json:"verb"`
	Token      string `json:"token"`
	BundlePath string `json:"bundle_path,omitempty"`
	Requester  string `json:"requester,omitempty"`
}

// Event is one line of an `update` verb's progress stream.
type Event struct {
	Type    string `json:"event"` // "stage" | "result"
	Stage   string `json:"stage,omitempty"`
	TS      string `json:"ts,omitempty"`
	OK      *bool  `json:"ok,omitempty"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
}

type CheckResponse struct {
	OK               bool    `json:"ok"`
	CurrentVersion   string  `json:"current_version"`
	AvailableVersion *string `json:"available_version"`
	DeliveryMode     string  `json:"delivery_mode"`
	BundlePath       *string `json:"bundle_path"`
}

type StatusResponse struct {
	OK        bool    `json:"ok"`
	Locked    bool    `json:"locked"`
	LockOwner *string `json:"lock_owner"`
	Stage     string  `json:"stage"`
	StartedAt *string `json:"started_at"`
}

type RollbackStatusResponse struct {
	OK          bool   `json:"ok"`
	LastAction  string `json:"last_action"`
	Result      string `json:"result"`
	Version     string `json:"version"`
	CompletedAt string `json:"completed_at"`
}

// ErrorResponse also carries the two "locked" fields (both nil otherwise) —
// one shape for every non-stage-stream failure, matching C2/C3's examples.
type ErrorResponse struct {
	OK        bool    `json:"ok"`
	Error     string  `json:"error"`
	LockOwner *string `json:"lock_owner,omitempty"`
	StartedAt *string `json:"started_at,omitempty"`
}

func writeLine(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = w.Write(b)
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
