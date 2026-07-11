package auth

import "testing"

func TestIPAllowed(t *testing.T) {
	cases := []struct {
		name  string
		ip    string
		allow []string
		want  bool
	}{
		{"empty list is unrestricted", "203.0.113.9", nil, true},
		{"in range", "192.168.1.50", []string{"192.168.1.0/24"}, true},
		{"out of range", "10.0.0.1", []string{"192.168.1.0/24"}, false},
		{"one of several", "10.8.0.3", []string{"192.168.1.0/24", "10.8.0.0/16"}, true},
		{"host /32 match", "198.51.100.7", []string{"198.51.100.7/32"}, true},
		{"host /32 miss", "198.51.100.8", []string{"198.51.100.7/32"}, false},
		{"ipv6 in range", "2001:db8::5", []string{"2001:db8::/32"}, true},
		{"malformed client ip fails closed", "not-an-ip", []string{"0.0.0.0/0"}, false},
		{"malformed cidr entry ignored", "10.0.0.1", []string{"garbage", "10.0.0.0/8"}, true},
	}
	for _, c := range cases {
		if got := ipAllowed(c.ip, c.allow); got != c.want {
			t.Errorf("%s: ipAllowed(%q,%v)=%v want %v", c.name, c.ip, c.allow, got, c.want)
		}
	}
}

func TestValidCIDR(t *testing.T) {
	for _, ok := range []string{"192.168.0.0/16", "10.0.0.1/32", "2001:db8::/48"} {
		if !validCIDR(ok) {
			t.Errorf("%s should be a valid CIDR", ok)
		}
	}
	for _, bad := range []string{"", "192.168.0.1", "not/a/cidr", "10.0.0.0/40"} {
		if validCIDR(bad) {
			t.Errorf("%s should be an invalid CIDR", bad)
		}
	}
}
