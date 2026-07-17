package vendor

// RouterOS API auto-setup for the MikroTik adapter (FR-56.2-56.4, contract
// C6). Every RouterOS API sentence HikRAD ever sends lives in this file (plus
// routeros_conn.go's transport) — the FR-17.1 vendor-isolation rule applies to
// auto-setup exactly as it does to CoA and the config snippet.
//
// Safety contract (frozen by Decision 17): PlanAutoSetup only ever issues
// /xxx/print (read) sentences. It classifies the router's current state
// against the FR-14.2 bootstrap config into three buckets:
//   - already present exactly as HikRAD wants it -> no item (nothing to do)
//   - absent, or present but "off"/default -> an additive PlanItem (add a new
//     list entry, or flip a singleton knob from its unconfigured default to
//     the value CoA/accounting needs)
//   - present with a DIFFERENT value that isn't HikRAD's own untouched
//     default -> a PlanConflict, because writing over it would be exactly the
//     silent overwrite FR-56.2 forbids
//
// ApplyAutoSetup only ever runs the exact sentences PlanAutoSetup computed,
// and the caller (internal/radius/autosetup_api.go) never calls it while
// Conflicts is non-empty.

import (
	"fmt"
	"strconv"
	"strings"
)

const hikradComment = "hikrad-auto"

// hotspotProfileName is the RouterOS hotspot profile auto-setup targets.
// MikroTik ships exactly one ("default") out of the box; a router with
// multiple custom profiles needs the FR-14 copy-paste path instead — the
// plan reports this explicitly as a conflict rather than guessing which
// profile terminates HikRAD's NAS.
const hotspotProfileName = "default"

// SupportsInPlace is the Phase 4 ROS quirk matrix (docs/ops/ros-matrix.md)
// encoded as code. Findings, both currently version-independent pending live
// pilot-hardware confirmation (kept version-parameterized so a future
// correction is a one-line change, not an API change):
//
//   - rate_limit: MikroTik applies an in-place Mikrotik-Rate-Limit CoA change
//     to an already-active PPP session on both 6.49 and 7.x. Hotspot sessions
//     do not reliably pick it up on either version (the dynamic queue backing
//     a Hotspot binding isn't re-evaluated by a bare CoA) -> Disconnect
//     fallback is used unconditionally for hotspot rather than racing a NAK.
//   - address_pool: Framed-Pool is read only at authentication time; MikroTik
//     never reassigns an already-active session's pool from a CoA-Request on
//     either version or service type. Every caller in this codebase already
//     treats a MovePool failure as a Disconnect-fallback trigger (billing
//     renewal, refund, enforcement), so always reporting "unsupported" here
//     just skips a guaranteed-to-fail 5s+retry round trip.
//   - session_timeout / redirect_expired: both are plain attribute/address-
//     list changes MikroTik applies in-place on any version.
func (mikrotikAdapter) SupportsInPlace(rosVersion, nasType, intent string) bool {
	switch intent {
	case IntentRateLimit:
		return nasType != "hotspot"
	case IntentAddressPool:
		return false
	case IntentSessionTimeout, IntentRedirectExpired:
		return true
	default:
		return false
	}
}

// PlanAutoSetup implements Adapter.PlanAutoSetup for MikroTik.
//
// resolutions (v2 phase 3, FR-66.2) resolves a conflict.Key to "update" or
// "keep"; anything else (including absent) means "abort" — an empty/nil map
// reproduces pre-FR-66 behavior exactly (C1's non-invalidation guarantee),
// because resolveConflict's default case is unchanged from the old
// unconditional "always a conflict" branch.
func (a mikrotikAdapter) PlanAutoSetup(conn ROSConn, in SnippetInput, resolutions map[string]string) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	add := func(item *PlanItem, conflict *PlanConflict) {
		resolveConflict(&plan, resolutions, item, conflict)
	}

	radiusItem, radiusConflict, err := a.planRadiusEntry(conn, in)
	if err != nil {
		return plan, err
	}
	add(radiusItem, radiusConflict)

	incomingItem, incomingConflict, err := a.planRadiusIncoming(conn, in)
	if err != nil {
		return plan, err
	}
	add(incomingItem, incomingConflict)

	// Plan each enabled service additively (C8/FR-62): a NAS running PPPoE and
	// two hotspot zones needs the PPP AAA entry AND each hotspot's profile, not
	// one or the other. Kinds are planned once each — /ppp aaa is global on
	// RouterOS, so N pppoe instances still yield one AAA item.
	services := in.services()
	if anyOfKind(services, "pppoe") {
		aaaItem, aaaConflict, err := a.planPPPAAA(conn, in)
		if err != nil {
			return plan, err
		}
		add(aaaItem, aaaConflict)
	}
	if anyOfKind(services, "hotspot") {
		profItem, profConflict, err := a.planHotspotProfile(conn, in)
		if err != nil {
			return plan, err
		}
		add(profItem, profConflict)

		gardenItems, err := a.planWalledGarden(conn, in)
		if err != nil {
			return plan, err
		}
		plan.Items = append(plan.Items, gardenItems...)
	}

	return plan, nil
}

// resolveConflict is the FR-66.2 decision table, shared by every planXXX call
// site so the resolution semantics live in exactly one place:
//   - no conflict: the additive item (if any) is appended, unchanged from
//     pre-FR-66 behavior.
//   - conflict + resolutions[key]=="update" + Resolvable: the conflict's
//     precomputed update sentence becomes a "set" PlanItem; the conflict is
//     dropped (nothing left to abort on for this item).
//   - conflict + resolutions[key]=="update" + !Resolvable: falls through to
//     the abort case below — an operator cannot force an update onto a
//     target HikRAD doesn't know how to compute.
//   - conflict + resolutions[key]=="keep": dropped entirely from both Items
//     and Conflicts — the operator explicitly accepted the router's current
//     state for this one item.
//   - conflict + anything else (unset/"abort"/unrecognized): reported as a
//     Conflict, identical to pre-FR-66 behavior — this is what keeps
//     len(plan.Conflicts) > 0 as the single abort gate.
func resolveConflict(plan *AutoSetupPlan, resolutions map[string]string, item *PlanItem, conflict *PlanConflict) {
	if conflict == nil {
		if item != nil {
			plan.Items = append(plan.Items, *item)
		}
		return
	}
	switch resolutions[conflict.Key] {
	case "update":
		if conflict.Resolvable {
			plan.Items = append(plan.Items, PlanItem{
				Action: "set", Path: conflict.Path, Command: conflict.UpdateCommand,
				CurrentState: conflict.Existing, Sentence: conflict.updateSentence,
			})
			return
		}
		plan.Conflicts = append(plan.Conflicts, *conflict)
	case "keep":
		// Accepted as-is; contributes nothing to either list.
	default:
		plan.Conflicts = append(plan.Conflicts, *conflict)
	}
}

// ApplyAutoSetup implements Adapter.ApplyAutoSetup for MikroTik.
func (mikrotikAdapter) ApplyAutoSetup(conn ROSConn, plan AutoSetupPlan) []ApplyResult {
	results := make([]ApplyResult, 0, len(plan.Items))
	for _, item := range plan.Items {
		res := ApplyResult{Path: item.Path, Command: item.Command}
		if _, err := conn.Write(item.Sentence...); err != nil {
			res.Error = err.Error()
		} else {
			res.OK = true
		}
		results = append(results, res)
		if !res.OK {
			break // whole-apply-abort-on-any-failure: stop issuing further writes
		}
	}
	return results
}

// --- /radius (the HikRAD AAA client entry) ---------------------------------

func (mikrotikAdapter) planRadiusEntry(conn ROSConn, in SnippetInput) (*PlanItem, *PlanConflict, error) {
	rows, err := conn.Read("/radius/print")
	if err != nil {
		return nil, nil, fmt.Errorf("mikrotik: read /radius: %w", err)
	}
	service := radiusService(in)
	for _, row := range rows {
		if row["address"] != in.RadiusServer || !strings.Contains(row["service"], strings.Split(service, ",")[0]) {
			continue
		}
		summary := fmt.Sprintf("address=%s service=%s comment=%q", row["address"], row["service"], row["comment"])
		// Both conflict cases below (FR-66.2) are resolvable: the target is
		// always "this exact /radius entry, rewritten to HikRAD's values" — an
		// update /set never has to guess which router object to touch.
		updateSentence := []string{"/radius/set", "=.id=" + row[".id"], "=service=" + service,
			"=address=" + in.RadiusServer, "=secret=" + in.Secret, "=comment=" + hikradComment}
		if in.SrcAddress != "" {
			updateSentence = append(updateSentence, "=src-address="+in.SrcAddress)
		}
		updateCmd := fmt.Sprintf("/radius set [find .id=%s] service=%s address=%s secret=**** comment=%s", row[".id"], service, in.RadiusServer, hikradComment)
		if strings.HasPrefix(row["comment"], hikradComment) {
			if row["secret"] == in.Secret || row["secret"] == "" {
				return nil, nil, nil // already ours, matches (or secret withheld by API perms) — no-op
			}
			return nil, &PlanConflict{
				Path: "/radius", Existing: summary, Key: "/radius",
				Reason:         "a HikRAD-tagged /radius entry already exists with a different secret; auto-setup never overwrites an existing entry",
				Resolvable:     true,
				UpdateCommand:  updateCmd,
				updateSentence: updateSentence,
			}, nil
		}
		return nil, &PlanConflict{
			Path: "/radius", Existing: summary, Key: "/radius",
			Reason:         "an existing /radius entry already points at this RADIUS server/service and was not created by HikRAD",
			Resolvable:     true,
			UpdateCommand:  updateCmd,
			updateSentence: updateSentence,
		}, nil
	}

	sentence := []string{"/radius/add", "=service=" + service, "=address=" + in.RadiusServer, "=secret=" + in.Secret, "=timeout=3s", "=comment=" + hikradComment}
	if in.SrcAddress != "" {
		sentence = append(sentence, "=src-address="+in.SrcAddress)
	}
	cmd := fmt.Sprintf("/radius add service=%s address=%s secret=**** comment=%s", service, in.RadiusServer, hikradComment)
	if in.SrcAddress != "" {
		cmd += " src-address=" + in.SrcAddress
	}
	return &PlanItem{Action: "add", Path: "/radius", Command: cmd, CurrentState: "not present", Sentence: sentence}, nil, nil
}

// radiusService is the /radius service list covering every kind the NAS
// terminates. It must render exactly what Snippet's shared /radius block does,
// or a router set up by the copy-paste path would look "wrong" to auto-setup
// (and vice versa) — the two paths describe one desired state.
func radiusService(in SnippetInput) string {
	services := in.services()
	var kinds []string
	if anyOfKind(services, "pppoe") {
		kinds = append(kinds, "ppp")
	}
	if anyOfKind(services, "hotspot") {
		kinds = append(kinds, "hotspot", "login")
	}
	if len(kinds) == 0 {
		return "ppp"
	}
	return strings.Join(kinds, ",")
}

// --- /radius/incoming (global CoA listener toggle) --------------------------

func (mikrotikAdapter) planRadiusIncoming(conn ROSConn, in SnippetInput) (*PlanItem, *PlanConflict, error) {
	rows, err := conn.Read("/radius/incoming/print")
	if err != nil {
		return nil, nil, fmt.Errorf("mikrotik: read /radius/incoming: %w", err)
	}
	coaPort := in.CoAPort
	if coaPort == 0 {
		coaPort = 3799
	}
	var accept, port string
	if len(rows) > 0 {
		accept, port = rows[0]["accept"], rows[0]["port"]
	}
	current := fmt.Sprintf("accept=%s port=%s", orDash(accept), orDash(port))

	wantPort := strconv.Itoa(coaPort)
	switch {
	case accept == "yes" && (port == wantPort || port == ""):
		return nil, nil, nil // already enabled on our port (or router hasn't reported a port — treat as match)
	case accept == "yes" && port != wantPort:
		return nil, &PlanConflict{
			Path: "/radius/incoming", Existing: current, Key: "/radius/incoming",
			Reason:         fmt.Sprintf("CoA is already enabled on port %s for another purpose; changing the router-wide CoA port to %s could break it", port, wantPort),
			Resolvable:     true,
			UpdateCommand:  fmt.Sprintf("/radius incoming set accept=yes port=%s", wantPort),
			updateSentence: []string{"/radius/incoming/set", "=accept=yes", "=port=" + wantPort},
		}, nil
	default: // accept == "no" or unset: safe to turn on, nothing existing is disturbed
		sentence := []string{"/radius/incoming/set", "=accept=yes", "=port=" + wantPort}
		cmd := fmt.Sprintf("/radius incoming set accept=yes port=%s", wantPort)
		return &PlanItem{Action: "set", Path: "/radius/incoming", Command: cmd, CurrentState: current, Sentence: sentence}, nil, nil
	}
}

// --- /ppp/aaa (PPPoE RADIUS AAA + accounting toggle) ------------------------

func (mikrotikAdapter) planPPPAAA(conn ROSConn, in SnippetInput) (*PlanItem, *PlanConflict, error) {
	rows, err := conn.Read("/ppp/aaa/print")
	if err != nil {
		return nil, nil, fmt.Errorf("mikrotik: read /ppp/aaa: %w", err)
	}
	interim := in.InterimSecs
	if interim == 0 {
		interim = 300
	}
	wantInterim := secs(interim)
	var useRadius, accounting, interimUpdate string
	if len(rows) > 0 {
		useRadius, accounting, interimUpdate = rows[0]["use-radius"], rows[0]["accounting"], rows[0]["interim-update"]
	}
	current := fmt.Sprintf("use-radius=%s accounting=%s interim-update=%s", orDash(useRadius), orDash(accounting), orDash(interimUpdate))

	switch {
	case useRadius == "yes" && accounting == "yes" && interimUpdate == wantInterim:
		return nil, nil, nil
	case useRadius == "yes" && (accounting != "" || interimUpdate != ""):
		// Already turned on with a different tuning someone else configured —
		// changing it would modify an existing, deliberately-set value.
		return nil, &PlanConflict{
			Path: "/ppp/aaa", Existing: current, Key: "/ppp/aaa",
			Reason:         "PPP AAA is already enabled with different accounting/interim-update settings than HikRAD needs",
			Resolvable:     true,
			UpdateCommand:  fmt.Sprintf("/ppp aaa set use-radius=yes accounting=yes interim-update=%s", wantInterim),
			updateSentence: []string{"/ppp/aaa/set", "=use-radius=yes", "=accounting=yes", "=interim-update=" + wantInterim},
		}, nil
	default:
		sentence := []string{"/ppp/aaa/set", "=use-radius=yes", "=accounting=yes", "=interim-update=" + wantInterim}
		cmd := fmt.Sprintf("/ppp aaa set use-radius=yes accounting=yes interim-update=%s", wantInterim)
		return &PlanItem{Action: "set", Path: "/ppp/aaa", Command: cmd, CurrentState: current, Sentence: sentence}, nil, nil
	}
}

// --- /ip/hotspot/profile (Hotspot RADIUS AAA toggle, per-version knob) ------

func (mikrotikAdapter) planHotspotProfile(conn ROSConn, in SnippetInput) (*PlanItem, *PlanConflict, error) {
	rows, err := conn.Read("/ip/hotspot/profile/print")
	if err != nil {
		return nil, nil, fmt.Errorf("mikrotik: read /ip/hotspot/profile: %w", err)
	}
	var prof map[string]string
	for _, row := range rows {
		if row["name"] == hotspotProfileName {
			prof = row
			break
		}
	}
	if prof == nil {
		// Not Resolvable (FR-66.2): there is no single profile to update on a
		// router with custom-named zones — guessing which one terminates
		// HikRAD's NAS would be exactly the silent-wrong-write FR-56.2 forbids.
		// FR-67's adopt flow is the answer for that router: the operator picks
		// the exact zone, and editing proceeds against a specific instance.
		return nil, &PlanConflict{
			Path: "/ip/hotspot/profile", Existing: "no profile named " + hotspotProfileName, Key: "/ip/hotspot/profile",
			Reason: "auto-setup only targets the default hotspot profile; a custom profile layout needs the FR-14 copy-paste snippet or FR-67's server adopt flow",
		}, nil
	}

	interim := in.InterimSecs
	if interim == 0 {
		interim = 300
	}
	ros7 := in.ROSVersion != "6"
	current := fmt.Sprintf("use-radius=%s", orDash(prof["use-radius"]))
	if prof["use-radius"] == "yes" {
		return nil, nil, nil // already on; leave whatever interim tuning is there (FR-56.2 never modifies an existing value)
	}

	sentence := []string{"/ip/hotspot/profile/set", "=.id=" + prof[".id"], "=use-radius=yes"}
	cmd := "/ip hotspot profile set [find name=" + hotspotProfileName + "] use-radius=yes "
	if ros7 {
		sentence = append(sentence, "=radius-interim-update="+secs(interim))
		cmd += "radius-interim-update=" + secs(interim)
	} else {
		sentence = append(sentence, "=radius-accounting=yes", "=interim-update="+secs(interim))
		cmd += "radius-accounting=yes interim-update=" + secs(interim)
	}
	return &PlanItem{Action: "set", Path: "/ip/hotspot/profile", Command: cmd, CurrentState: current, Sentence: sentence}, nil, nil
}

// --- /ip/hotspot/user/profile (the login-time address source) ---------------

// planHotspotUserProfilePool clears the address-pool on the DEFAULT hotspot user
// profile, which is where every RADIUS-authenticated hotspot user lands (HikRAD
// sends no Mikrotik-Group).
//
// That profile's address-pool OVERRIDES the hotspot server's own, so whatever it
// names is what the router tries to allocate at login. If it names a pool that
// no longer exists, EVERY HikRAD hotspot login on the router fails with "no
// address from ip pool" while RADIUS reports a clean accept — the 2026-07-16
// pilot outage (docs/ops/known-issues.md).
//
// This is an exception to FR-56.2's "never modify an existing value", and a
// deliberate one: the existing value is precisely the fault, the operator sees
// it in the preview before anything is written, and it cannot be fixed from the
// GUI — Winbox resolves a pool id to a name, so a DANGLING id renders as an
// empty "none" and an operator who checks it is told everything is fine.
// Requiring them to hand-type a CLI command to escape a trap they cannot see is
// not a fix.
//
// none is correct either way: with no HikRAD pool the client keeps the address
// the hotspot server already gave it; with one, Framed-Pool wins regardless.
func (mikrotikAdapter) planHotspotUserProfilePool(conn ROSConn) (*PlanItem, error) {
	pool, exists, err := defaultHotspotUserProfilePool(conn)
	if err != nil {
		// No /ip hotspot on this box; nothing to plan.
		return nil, nil
	}
	if pool == "" {
		return nil, nil // already none — the healthy state.
	}
	current := fmt.Sprintf("address-pool=%s", pool)
	if !exists {
		current += " (this pool does not exist — every hotspot login is failing)"
	}
	return &PlanItem{
		Action:       "set",
		Path:         "/ip/hotspot/user/profile",
		Command:      "/ip hotspot user profile set [find default=yes] address-pool=none",
		CurrentState: current,
		Sentence:     []string{"/ip/hotspot/user/profile/set", "=.id=*0", "=address-pool=none"},
	}, nil
}

// --- /ip/hotspot/walled-garden (per-host additive entries) -----------------

func (mikrotikAdapter) planWalledGarden(conn ROSConn, in SnippetInput) ([]PlanItem, error) {
	rows, err := conn.Read("/ip/hotspot/walled-garden/print")
	if err != nil {
		return nil, fmt.Errorf("mikrotik: read /ip/hotspot/walled-garden: %w", err)
	}
	existing := map[string]bool{}
	for _, row := range rows {
		if row["dst-host"] != "" {
			existing[row["dst-host"]] = true
		}
	}
	var items []PlanItem
	for _, host := range in.WalledGarden {
		host = strings.TrimSpace(host)
		if host == "" || existing[host] {
			continue
		}
		sentence := []string{"/ip/hotspot/walled-garden/add", "=dst-host=" + host, "=action=allow", "=comment=" + hikradComment}
		cmd := "/ip hotspot walled-garden add dst-host=" + host + " action=allow"
		items = append(items, PlanItem{Action: "add", Path: "/ip/hotspot/walled-garden", Command: cmd, CurrentState: "not present", Sentence: sentence})
	}
	return items, nil
}

func orDash(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}
