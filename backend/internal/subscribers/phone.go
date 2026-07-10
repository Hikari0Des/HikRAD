package subscribers

// Iraqi phone normalization (FR-1.3). Operators enter numbers in either the
// local trunk form (07XX XXX XXXX) or the international form (+9647XX XXX XXXX);
// both are stored canonically as +9647XXXXXXXX so search and de-dup are exact.
// Arabic/Eastern-Arabic digits and common separators are accepted on input.

import "strings"

// easternArabicDigits maps ٠..٩ (both Arabic-Indic and Extended/Persian ranges)
// to ASCII so a number typed on an Arabic keyboard normalizes identically.
var easternArabicDigits = map[rune]rune{
	'٠': '0', '١': '1', '٢': '2', '٣': '3', '٤': '4',
	'٥': '5', '٦': '6', '٧': '7', '٨': '8', '٩': '9',
	'۰': '0', '۱': '1', '۲': '2', '۳': '3', '۴': '4',
	'۵': '5', '۶': '6', '۷': '7', '۸': '8', '۹': '9',
}

// normalizePhone canonicalizes an Iraqi mobile number to +9647XXXXXXXX, or
// returns ok=false when the input is not a recognizable Iraqi mobile number.
// An empty input is allowed (phone is optional) and returns ("", true).
func normalizePhone(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	// Extract digits, translating Eastern-Arabic numerals and dropping every
	// separator (spaces, dashes, parentheses). Keep a leading + marker.
	var b strings.Builder
	plus := false
	for _, r := range raw {
		if r == '+' {
			plus = true
			continue
		}
		if a, ok := easternArabicDigits[r]; ok {
			r = a
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()

	// Strip a country code to the 10-digit national number (7XXXXXXXX+subscriber).
	switch {
	case plus && strings.HasPrefix(d, "964"):
		d = strings.TrimPrefix(d, "964")
	case strings.HasPrefix(d, "00964"):
		d = strings.TrimPrefix(d, "00964")
	case strings.HasPrefix(d, "964") && len(d) == 13:
		d = strings.TrimPrefix(d, "964")
	case strings.HasPrefix(d, "0"):
		d = strings.TrimPrefix(d, "0")
	}

	// Iraqi mobile national numbers are 10 digits beginning with 7.
	if len(d) != 10 || d[0] != '7' {
		return "", false
	}
	return "+964" + d, true
}
