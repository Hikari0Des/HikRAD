package radius

// DedupeScopes decides what a saved FR-64 scope set actually contains, so its
// collapse rule is worth pinning: it is the difference between an operator's
// picker showing a coherent list and showing "this NAS" alongside "only the
// Lobby zone on this NAS".

import (
	"reflect"
	"testing"
)

func TestDedupeScopes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []NASScope
		want []NASScope
	}{
		{
			name: "exact duplicates collapse",
			in:   []NASScope{{NASID: "n1"}, {NASID: "n1"}},
			want: []NASScope{{NASID: "n1"}},
		},
		{
			// The whole-NAS row already allows every zone on it, so keeping the
			// per-service rows would show a contradictory list.
			name: "a whole-NAS scope absorbs its per-service scopes",
			in:   []NASScope{{NASID: "n1", ServiceID: "svc-a"}, {NASID: "n1"}, {NASID: "n1", ServiceID: "svc-b"}},
			want: []NASScope{{NASID: "n1"}},
		},
		{
			// ...but only on its OWN NAS. Absorbing across NASes would silently
			// widen n2 from one zone to all of them.
			name: "a whole-NAS scope does not absorb another NAS's service scopes",
			in:   []NASScope{{NASID: "n1"}, {NASID: "n2", ServiceID: "svc-a"}},
			want: []NASScope{{NASID: "n1"}, {NASID: "n2", ServiceID: "svc-a"}},
		},
		{
			name: "distinct services on one NAS are all kept",
			in:   []NASScope{{NASID: "n1", ServiceID: "svc-a"}, {NASID: "n1", ServiceID: "svc-b"}},
			want: []NASScope{{NASID: "n1", ServiceID: "svc-a"}, {NASID: "n1", ServiceID: "svc-b"}},
		},
		{
			// An empty set is "any NAS" and must survive as one — never become a
			// scope that denies.
			name: "empty stays empty",
			in:   nil,
			want: []NASScope{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DedupeScopes(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DedupeScopes(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// The collapse must not change what authenticates: whatever a raw set allows,
// its deduped form must allow identically.
func TestDedupeScopesPreservesWhatAuthenticates(t *testing.T) {
	raw := []NASScope{{NASID: "n1", ServiceID: "svc-a"}, {NASID: "n1"}, {NASID: "n2", ServiceID: "svc-b"}}
	deduped := DedupeScopes(raw)
	for _, probe := range []struct{ nas, svc string }{
		{"n1", "svc-a"}, {"n1", "svc-z"}, {"n2", "svc-b"}, {"n2", "svc-a"}, {"n3", "svc-a"},
	} {
		if scopeAllows(raw, probe.nas, probe.svc) != scopeAllows(deduped, probe.nas, probe.svc) {
			t.Errorf("dedupe changed the decision for nas=%s svc=%s", probe.nas, probe.svc)
		}
	}
}
