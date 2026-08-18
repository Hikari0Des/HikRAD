package cards

// Static guard for gate item 10 ("cards use the v4-4 engine... grep confirms
// no second limit implementation"). This package must resolve a card's
// accept/reject-relevant DATA (usage vs. limits, expiry) but must NEVER
// decide accept/reject itself — that stays radius/limits.go's evaluateLimits,
// the ONE implementation the v4-4 phase brief froze. This test greps this
// package's own source for the tell-tale shape of a second implementation:
// an unconditional early return that maps directly to a RADIUS reject
// reason, or a local redefinition of the evaluation order. It intentionally
// does NOT forbid computing booleans like quotaExhausted/isExpired — those
// are DATA the engine consumes, identical in spirit to
// subscribers/authview.go computing QuotaExhausted for a subscriber.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// forbiddenPatterns match strings that would only appear in a REJECT-DECIDING
// implementation: the radius package's own reason constants used as if this
// package were building an authorizeResponse itself (it never should — it
// returns AuthView data to radius.CardAuthenticator, and radius alone turns
// that into accept/reject).
var forbiddenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`radius\.Reason\w+`),
	regexp.MustCompile(`(?i)authorizeResponse`),
	regexp.MustCompile(`(?i)\bAction:\s*"reject"`),
}

func TestNoSecondEnforcementImplementation(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, pat := range forbiddenPatterns {
			if pat.Match(body) {
				t.Errorf("%s matches %s — cards must only build AuthView DATA "+
					"(radius.AuthView fields) and let radius/limits.go's evaluateLimits "+
					"decide accept/reject; a match here suggests a second enforcement "+
					"implementation", e.Name(), pat.String())
			}
		}
	}
}
