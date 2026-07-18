// Package updates is hikrad-api's relay to hikrad-updaterd (v2 phase 7,
// FR-86/87) — see the package doc comment on protocol.go for the split of
// responsibility between the two. Every route requires the system.update
// permission (C5): admin gets it via the existing wildcard role; no other
// builtin role is granted it, matching FR-87.1's admin-only-by-default
// posture. A custom DB-backed role can be granted it through the existing
// role editor with zero code change here, same as every other permission
// string in this codebase.
package updates

import (
	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type Module struct{}

func (Module) Name() string { return "updates" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	r.With(auth.Require("system.update")).Get("/api/v1/system/update/check", checkHandler)
	r.With(auth.Require("system.update")).Post("/api/v1/system/update", updateHandler)
	r.With(auth.Require("system.update")).Get("/api/v1/system/update/stream", streamHandler)
	r.With(auth.Require("system.update")).Get("/api/v1/system/update/status", statusHandler)
}

func init() { httpapi.Add(Module{}) }
