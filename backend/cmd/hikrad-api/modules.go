package main

// Blank-import list frozen by Phase-1 contract C3: every domain package
// self-registers its HTTP module via httpapi.Add in init(), so mounting a
// package means adding one line here and nothing else — no shared route
// file is ever edited.
//
// Planned packages (C3): platform, auth, radius, subscribers, profiles,
// billing, portalapi, reports, live. Only the packages that exist this
// phase are imported; each future package's owner uncomments its line in
// the phase that creates it.
import (
	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/billing"  // Phase 3
	_ "github.com/hikrad/hikrad/internal/importer" // Phase 5
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/monitorsvc"
	_ "github.com/hikrad/hikrad/internal/platform"
	_ "github.com/hikrad/hikrad/internal/platform/setupapi" // Phase 5
	_ "github.com/hikrad/hikrad/internal/portalapi"         // Phase 4
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/push" // Phase 4
	_ "github.com/hikrad/hikrad/internal/radius"
	_ "github.com/hikrad/hikrad/internal/reports" // Phase 5
	_ "github.com/hikrad/hikrad/internal/subscribers"
)
