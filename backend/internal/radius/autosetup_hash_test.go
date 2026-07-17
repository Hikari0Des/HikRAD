package radius

// Pure-function tests for planHash (FR-66.3, contract C6/gate item 5): no DB
// or router needed — these assert the hash is a function of Values and
// Resolutions, not only of the resulting plan.

import (
	"testing"

	"github.com/hikrad/hikrad/internal/radius/vendor"
)

func TestPlanHash_DiffersAcrossValues(t *testing.T) {
	plan := vendor.AutoSetupPlan{}
	a := "1.2.3.4"
	b := "5.6.7.8"
	h1 := planHash("nas-1", autoSetupValuesInput{RadiusServer: &a}, nil, plan)
	h2 := planHash("nas-1", autoSetupValuesInput{RadiusServer: &b}, nil, plan)
	if h1 == h2 {
		t.Fatalf("expected different hashes for different Values with an identical plan, got the same: %s", h1)
	}
}

func TestPlanHash_DiffersAcrossResolutions(t *testing.T) {
	plan := vendor.AutoSetupPlan{}
	h1 := planHash("nas-1", autoSetupValuesInput{}, map[string]string{"/radius": "update"}, plan)
	h2 := planHash("nas-1", autoSetupValuesInput{}, map[string]string{"/radius": "keep"}, plan)
	if h1 == h2 {
		t.Fatalf("expected different hashes for different Resolutions with an identical plan, got the same: %s", h1)
	}
}

func TestPlanHash_DeterministicForSameInput(t *testing.T) {
	plan := vendor.AutoSetupPlan{Items: []vendor.PlanItem{{Action: "add", Path: "/radius", Command: "x"}}}
	res := map[string]string{"/radius/incoming": "update"}
	h1 := planHash("nas-1", autoSetupValuesInput{}, res, plan)
	h2 := planHash("nas-1", autoSetupValuesInput{}, res, plan)
	if h1 != h2 {
		t.Fatalf("expected a deterministic hash for identical input, got %s vs %s", h1, h2)
	}
}

func TestPlanHash_DiffersAcrossNAS(t *testing.T) {
	plan := vendor.AutoSetupPlan{}
	h1 := planHash("nas-1", autoSetupValuesInput{}, nil, plan)
	h2 := planHash("nas-2", autoSetupValuesInput{}, nil, plan)
	if h1 == h2 {
		t.Fatalf("expected different hashes across NAS ids, got the same: %s", h1)
	}
}
