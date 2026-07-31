package importer

import "strings"

// Column mapping + the SAS4 export preset (FR-6). The map is always
// {hikrad_field: source_column_name}; a preset is just a starting guess the
// operator confirms/adjusts in the mapping UI (E) before dry-run — SAS4's
// export columns vary by version (sub-PRD 04 §7 open question), so this is a
// best-effort default, not a hard contract.

// subscriberFields are the subscriber attributes the wizard understands. Only
// "username" is required; everything else is optional per row.
var subscriberFields = map[string]bool{
	"username":   true,
	"password":   true,
	"name":       true,
	"phone":      true,
	"address":    true,
	"profile":    true, // profile NAME; resolved to profile_id during validation
	"expires_at": true,
	// service_type (FR-61): pppoe | hotspot | dual. Accepts a SAS4-style
	// boolean hotspot column too — see normalizeServiceType. Omitted → pppoe.
	"service_type": true,
	"email":        true, // FR-85
}

// profileFields are the plan attributes the wizard understands (FR-111.3). Only
// "name" is required. Rates accept the FR-95 prefix forms (10M/512k/bare kbps).
var profileFields = map[string]bool{
	"name":              true,
	"price":             true,
	"currency":          true,
	"duration_days":     true,
	"rate_down":         true,
	"rate_up":           true,
	"session_limit":     true,
	"quota_mode":        true,
	"quota_total_bytes": true,
	"expiry_behavior":   true,
	"quota_behavior":    true,
	"address_list":      true,
}

// fieldsFor returns the mappable field set for an entity.
func fieldsFor(entity string) map[string]bool {
	if entity == "profiles" {
		return profileFields
	}
	return subscriberFields
}

// requiredField is the one column an entity's import cannot proceed without.
func requiredField(entity string) string {
	if entity == "profiles" {
		return "name"
	}
	return "username"
}

// presets maps entity → preset name → default column_map. Column names are
// matched case-insensitively against the uploaded file's header.
var presets = map[string]map[string]map[string]string{
	"subscribers": {
		"sas4": {
			"username":   "UserName",
			"password":   "Password",
			"name":       "FullName",
			"phone":      "Mobile",
			"address":    "Address",
			"profile":    "Package",
			"expires_at": "ExpireDate",
			"email":      "Email", // matched case-insensitively, only if present in the uploaded header
		},
	},
	"profiles": {
		// A best-effort default matching common plan-export column names; the
		// operator confirms/adjusts before dry-run like every other preset.
		"sas4": {
			"name":              "Name",
			"price":             "Price",
			"currency":          "Currency",
			"duration_days":     "Days",
			"rate_down":         "DownloadRate",
			"rate_up":           "UploadRate",
			"quota_total_bytes": "Quota",
		},
	},
}

// normalizeServiceType maps an imported cell onto an FR-61 service_type. It
// accepts the three canonical values and, because no other system models a
// three-valued service, the boolean "is this a hotspot user?" column a SAS4-era
// export actually carries — mapped exactly as migration 0500 maps v1's own
// allow_hotspot bit (true → dual, false → pppoe) so an import and an upgrade
// agree. An operator who means hotspot-ONLY writes "hotspot" explicitly.
func normalizeServiceType(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pppoe", "ppp":
		return "pppoe", true
	case "hotspot":
		return "hotspot", true
	case "dual", "both":
		return "dual", true
	case "true", "yes", "y", "1":
		return "dual", true
	case "false", "no", "n", "0":
		return "pppoe", true
	}
	return "", false
}

// resolvePreset returns the column_map for a named preset (within an entity)
// restricted to columns actually present in header (case-insensitive), or nil
// for an unknown/empty preset name.
func resolvePreset(entity, name string, header []string) map[string]string {
	byName, ok := presets[entity]
	if !ok {
		return nil
	}
	base, ok := byName[name]
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
