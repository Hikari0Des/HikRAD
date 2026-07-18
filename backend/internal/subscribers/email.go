package subscribers

// Email validation (FR-85.1): a simple RFC-5322-shaped check — one '@', a
// non-empty local part, a domain part containing at least one '.', no
// whitespace — not a full grammar, matching the phone validator's "reject
// clearly wrong, don't over-engineer" posture (email is not a credential and
// carries no encryption at rest, unlike the RADIUS password).

import "strings"

// validateEmail reports whether s is a plausible email address. An empty
// input is allowed (email is optional).
func validateEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return false
	}
	local, domain := s[:at], s[at+1:]
	if local == "" || domain == "" {
		return false
	}
	dot := strings.LastIndex(domain, ".")
	if dot <= 0 || dot == len(domain)-1 {
		return false
	}
	return true
}
