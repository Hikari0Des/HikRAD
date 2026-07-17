package vendor

// RouterOS config inspection for the MikroTik adapter (v2 phase 3, FR-65,
// contract C2). ReadConfig issues exactly the same print sentences
// PlanAutoSetup's planXXX helpers already read — it exists so the panel can
// show the operator what the router has BEFORE they fill in the FR-66 values
// form, using the identical read seam (no separate "inspection" code path
// that could drift from what the planner actually sees).
//
// Pure read: never writes, same ROSConn contract as PlanAutoSetup/
// DiscoverServices/CheckHealth.

import (
	"fmt"
	"strconv"
)

// ReadConfig implements Adapter.ReadConfig for MikroTik.
func (mikrotikAdapter) ReadConfig(conn ROSConn) (ConfigSnapshot, error) {
	var snap ConfigSnapshot

	radiusRows, err := conn.Read("/radius/print")
	if err != nil {
		return snap, fmt.Errorf("mikrotik: read /radius: %w", err)
	}
	for _, row := range radiusRows {
		snap.RadiusEntries = append(snap.RadiusEntries, RadiusEntryConfig{
			Address: row["address"], Service: row["service"], Comment: row["comment"],
			SrcAddress: row["src-address"], SecretPresent: row["secret"] != "",
		})
	}

	incomingRows, err := conn.Read("/radius/incoming/print")
	if err != nil {
		return snap, fmt.Errorf("mikrotik: read /radius/incoming: %w", err)
	}
	if len(incomingRows) > 0 {
		port, _ := strconv.Atoi(incomingRows[0]["port"])
		snap.RadiusIncoming = RadiusIncomingConfig{Accept: incomingRows[0]["accept"] == "yes", Port: port}
	}

	aaaRows, err := conn.Read("/ppp/aaa/print")
	if err != nil {
		return snap, fmt.Errorf("mikrotik: read /ppp/aaa: %w", err)
	}
	if len(aaaRows) > 0 {
		snap.PPPAAA = PPPAAAConfig{
			UseRadius: aaaRows[0]["use-radius"] == "yes", Accounting: aaaRows[0]["accounting"] == "yes",
			InterimUpdateSecs: parseSecs(aaaRows[0]["interim-update"]),
		}
	}

	profileRows, err := conn.Read("/ip/hotspot/profile/print")
	if err != nil {
		return snap, fmt.Errorf("mikrotik: read /ip/hotspot/profile: %w", err)
	}
	for _, row := range profileRows {
		interim := row["radius-interim-update"]
		if interim == "" {
			interim = row["interim-update"]
		}
		snap.HotspotProfiles = append(snap.HotspotProfiles, HotspotProfileConfig{
			Name: row["name"], UseRadius: row["use-radius"] == "yes", InterimUpdateSecs: parseSecs(interim),
		})
	}

	gardenRows, err := conn.Read("/ip/hotspot/walled-garden/print")
	if err != nil {
		return snap, fmt.Errorf("mikrotik: read /ip/hotspot/walled-garden: %w", err)
	}
	for _, row := range gardenRows {
		if row["dst-host"] != "" {
			snap.WalledGarden = append(snap.WalledGarden, row["dst-host"])
		}
	}

	return snap, nil
}

// parseSecs turns a RouterOS duration like "5m" or "300s" into whole seconds.
// Best-effort: an unparseable/empty value yields 0 rather than erroring —
// this is a display field, never something ReadConfig's caller reasons about.
func parseSecs(s string) int {
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	// RouterOS renders "5m", "3h", "300s" etc.; secs() elsewhere in this
	// package only ever WRITES that format, so parse the common single-unit
	// case rather than pull in a duration parser for a cosmetic field.
	var n int
	var unit byte
	if k, err := fmt.Sscanf(s, "%d%c", &n, &unit); err == nil && k == 2 {
		switch unit {
		case 's':
			return n
		case 'm':
			return n * 60
		case 'h':
			return n * 3600
		}
	}
	return 0
}
