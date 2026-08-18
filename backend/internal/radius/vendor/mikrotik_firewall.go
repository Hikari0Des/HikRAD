package vendor

// FR-146 Firewall tab: read + plan + pre-change snapshot for the MikroTik
// adapter. The auto-revert safety net itself (the timed-deadline logic and
// the worker that actually re-applies a stored snapshot after a timeout) is
// NOT here — it belongs to internal/radius (module.go/firewall_api.go),
// which owns the nas_firewall_snapshots table (0612) and the clock. This
// file only ever does two things a caller can trust in isolation: read the
// router's real rules, and capture "what these specific rules looked like
// right now" as data, never inferred later from a possibly-unreachable
// router.

import (
	"fmt"
	"sort"
	"strings"
)

// sortedFieldKeys returns fields' keys sorted, so a FirewallChangeInput's
// Command/Sentence rendering is deterministic — map iteration order is not,
// and a plan re-computed at apply time from the SAME request must produce
// the byte-identical command preview saw, or every firewall write would
// spuriously fail preview_stale (autosetup_api.go's tamper-safety hash).
func sortedFieldKeys(fields map[string]string) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ReadFirewallRules implements Adapter.ReadFirewallRules.
func (a mikrotikAdapter) ReadFirewallRules(conn ROSConn) ([]FirewallRule, error) {
	var out []FirewallRule
	filterRows, err := conn.Read("/ip/firewall/filter/print")
	if err != nil {
		return nil, fmt.Errorf("mikrotik: read /ip/firewall/filter: %w", err)
	}
	out = append(out, firewallRowsToRules(filterRows, "filter")...)

	natRows, err := conn.Read("/ip/firewall/nat/print")
	if err != nil {
		return nil, fmt.Errorf("mikrotik: read /ip/firewall/nat: %w", err)
	}
	out = append(out, firewallRowsToRules(natRows, "nat")...)
	return out, nil
}

func firewallRowsToRules(rows []map[string]string, table string) []FirewallRule {
	out := make([]FirewallRule, 0, len(rows))
	for _, r := range rows {
		ownership := "router"
		if strings.Contains(r["comment"], hikradComment) {
			ownership = "system"
		}
		out = append(out, FirewallRule{
			ID: r[".id"], Chain: r["chain"], Table: table, Action: r["action"],
			SrcAddress: r["src-address"], DstAddress: r["dst-address"],
			OutInterface: r["out-interface"], InInterface: r["in-interface"],
			Protocol: r["protocol"], DstPort: r["dst-port"], Comment: r["comment"],
			Disabled: r["disabled"] == "true", Ownership: ownership,
		})
	}
	return out
}

// PlanFirewallChange implements Adapter.PlanFirewallChange. A new rule
// (RuleID empty) is always additive; an edit to an existing rule requires it
// to carry the hikrad-auto comment tag (system-owned) — editing a router-
// adopted rule the operator has not tagged as HikRAD's own is refused as a
// conflict, mirroring the router-ownership rule everywhere else in this
// phase, rather than silently rewriting someone else's rule.
func (a mikrotikAdapter) PlanFirewallChange(conn ROSConn, in FirewallChangeInput) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	path := "/ip/firewall/" + in.Table
	rows, err := conn.Read(path + "/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read %s: %w", path, err)
	}

	if in.RuleID == "" {
		sentence := []string{path + "/add", "=comment=" + hikradComment}
		var cmdFields []string
		for _, k := range sortedFieldKeys(in.Fields) {
			v := in.Fields[k]
			sentence = append(sentence, "="+k+"="+v)
			cmdFields = append(cmdFields, k+"="+v)
		}
		plan.Items = append(plan.Items, PlanItem{
			Action: "add", Path: path,
			Command:      fmt.Sprintf("%s add %s", strings.ReplaceAll(path, "/", " "), strings.Join(cmdFields, " ")),
			CurrentState: "not present", Sentence: sentence,
		})
		return plan, nil
	}

	var existing map[string]string
	for _, r := range rows {
		if r[".id"] == in.RuleID {
			existing = r
			break
		}
	}
	if existing == nil {
		plan.Conflicts = append(plan.Conflicts, PlanConflict{
			Path: path, Existing: "rule not found", Key: path + ":" + in.RuleID,
			Reason: "the rule to edit no longer exists on the router — it may have been removed outside HikRAD",
		})
		return plan, nil
	}
	if !strings.Contains(existing["comment"], hikradComment) {
		plan.Conflicts = append(plan.Conflicts, PlanConflict{
			Path: path, Existing: summarizeRule(existing), Key: path + ":" + in.RuleID,
			Reason: "this rule is router-managed (not created by HikRAD) and cannot be edited until adopted",
		})
		return plan, nil
	}
	sentence := []string{path + "/set", "=.id=" + in.RuleID}
	var cmdFields []string
	for _, k := range sortedFieldKeys(in.Fields) {
		v := in.Fields[k]
		sentence = append(sentence, "="+k+"="+v)
		cmdFields = append(cmdFields, k+"="+v)
	}
	plan.Items = append(plan.Items, PlanItem{
		Action: "set", Path: path,
		Command:      fmt.Sprintf("%s set [find .id=%s] %s", strings.ReplaceAll(path, "/", " "), in.RuleID, strings.Join(cmdFields, " ")),
		CurrentState: summarizeRule(existing), Sentence: sentence,
	})
	return plan, nil
}

func summarizeRule(r map[string]string) string {
	return fmt.Sprintf("chain=%s action=%s comment=%q", r["chain"], r["action"], r["comment"])
}

// SnapshotFirewallRules implements Adapter.SnapshotFirewallRules. It re-reads
// the CURRENT value of every rule plan.Items references by ID (a "set" item
// only — a pending "add" has no prior state to restore, so it contributes no
// revert sentence: undoing an add-that-hasn't-happened-yet is a no-op by
// construction, not a gap) and computes the exact /set sentence that would
// put each one back exactly as it is right now, BEFORE apply changes
// anything.
func (a mikrotikAdapter) SnapshotFirewallRules(conn ROSConn, plan AutoSetupPlan) (FirewallSnapshot, error) {
	var snap FirewallSnapshot
	tables := map[string]bool{}
	ids := map[string]bool{}
	for _, item := range plan.Items {
		if item.Action != "set" {
			continue
		}
		table := strings.TrimPrefix(item.Path, "/ip/firewall/")
		tables[table] = true
		for _, w := range item.Sentence {
			if strings.HasPrefix(w, "=.id=") {
				ids[strings.TrimPrefix(w, "=.id=")] = true
			}
		}
	}
	for table := range tables {
		rows, err := conn.Read("/ip/firewall/" + table + "/print")
		if err != nil {
			return snap, fmt.Errorf("mikrotik: snapshot read /ip/firewall/%s: %w", table, err)
		}
		for _, r := range rows {
			if !ids[r[".id"]] {
				continue
			}
			snap.Rules = append(snap.Rules, firewallRowsToRules([]map[string]string{r}, table)[0])
			revert := []string{"/ip/firewall/" + table + "/set", "=.id=" + r[".id"]}
			for _, field := range []string{"chain", "action", "src-address", "dst-address", "out-interface", "in-interface", "protocol", "dst-port", "disabled", "comment"} {
				if v, ok := r[field]; ok && v != "" {
					revert = append(revert, "="+field+"="+v)
				}
			}
			snap.RevertSentences = append(snap.RevertSentences, revert)
		}
	}
	return snap, nil
}
