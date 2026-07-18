package importer

// validateEmailLocal mirrors subscribers/email.go's validation (FR-85.2).
// Duplicated rather than imported: subscribers' validateEmail is unexported
// and subscribers is read-only to this package (file ownership) — dry-run
// only needs to know whether a value WOULD pass, the real create call
// re-validates authoritatively via the subscribers API.

import "strings"

func validateEmailLocal(s string) bool {
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
