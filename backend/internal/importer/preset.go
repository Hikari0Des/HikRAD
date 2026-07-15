package importer

import "strings"

// Column mapping + the SAS4 export preset (FR-6). The map is always
// {hikrad_field: source_column_name}; a preset is just a starting guess the
// operator confirms/adjusts in the mapping UI (E) before dry-run — SAS4's
// export columns vary by version (sub-PRD 04 §7 open question), so this is a
// best-effort default, not a hard contract.

// hikradFields are the subscriber attributes the wizard understands. Only
// "username" is required; everything else is optional per row.
var hikradFields = map[string]bool{
	"username":   true,
	"password":   true,
	"name":       true,
	"phone":      true,
	"address":    true,
	"profile":    true, // profile NAME; resolved to profile_id during validation
	"expires_at": true,
}

// presets maps a preset name to its default column_map. Column names are
// matched case-insensitively against the uploaded file's header.
var presets = map[string]map[string]string{
	"sas4": {
		"username":   "UserName",
		"password":   "Password",
		"name":       "FullName",
		"phone":      "Mobile",
		"address":    "Address",
		"profile":    "Package",
		"expires_at": "ExpireDate",
	},
}

// resolvePreset returns the column_map for a named preset restricted to
// columns actually present in header (case-insensitive), or nil for an
// unknown/empty preset name.
func resolvePreset(name string, header []string) map[string]string {
	base, ok := presets[name]
	if !ok {
		return nil
	}
	byLower := make(map[string]string, len(header))
	for _, h := range header {
		byLower[strings.ToLower(h)] = h
	}
	out := map[string]string{}
	for field, want := range base {
		if actual, ok := byLower[strings.ToLower(want)]; ok {
			out[field] = actual
		}
	}
	return out
}
