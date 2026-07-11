package vendor

import "testing"

func TestComposeRate(t *testing.T) {
	a := mikrotikAdapter{}
	cases := []struct {
		name string
		spec RateSpec
		want string
	}{
		{"empty", RateSpec{}, ""},
		{"base only", RateSpec{Rate: "10M/10M"}, "10M/10M"},
		{"partial burst ignored", RateSpec{Rate: "10M/10M", BurstRate: "20M/20M"}, "10M/10M"},
		{"full burst", RateSpec{
			Rate: "10M/10M", BurstRate: "20M/20M", BurstThreshold: "15M/15M", BurstTime: "16/16",
		}, "10M/10M 20M/20M 15M/15M 16/16"},
		{"burst with priority", RateSpec{
			Rate: "10M/10M", BurstRate: "20M/20M", BurstThreshold: "15M/15M", BurstTime: "16/16", Priority: "8",
		}, "10M/10M 20M/20M 15M/15M 16/16 8"},
		{"burst with priority and min", RateSpec{
			Rate: "10M/10M", BurstRate: "20M/20M", BurstThreshold: "15M/15M", BurstTime: "16/16", Priority: "1", MinRate: "1M/1M",
		}, "10M/10M 20M/20M 15M/15M 16/16 1 1M/1M"},
		{"burst with min but no priority defaults priority 8", RateSpec{
			Rate: "10M/10M", BurstRate: "20M/20M", BurstThreshold: "15M/15M", BurstTime: "16/16", MinRate: "1M/1M",
		}, "10M/10M 20M/20M 15M/15M 16/16 8 1M/1M"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := a.ComposeRate(c.spec); got != c.want {
				t.Errorf("ComposeRate = %q, want %q", got, c.want)
			}
		})
	}
}

// The adapter For() fallback must expose ComposeRate too.
func TestComposeRate_ViaRegistry(t *testing.T) {
	if got := For("mikrotik").ComposeRate(RateSpec{Rate: "5M/5M"}); got != "5M/5M" {
		t.Errorf("via registry = %q", got)
	}
	if got := For("unknown").ComposeRate(RateSpec{Rate: "5M/5M"}); got != "5M/5M" {
		t.Errorf("via unknown fallback = %q", got)
	}
}
