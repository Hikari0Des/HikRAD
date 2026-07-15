package importer

// normalizePhoneLocal mirrors subscribers/phone.go's Iraqi mobile validation
// (+9647XXXXXXXX canonical form, Eastern-Arabic digit support). Duplicated
// rather than imported: subscribers' normalizePhone is unexported and
// subscribers is read-only to this package (file ownership) — dry-run only
// needs to know whether a value WOULD pass, the real create call re-validates
// authoritatively via the subscribers API.

import "strings"

var easternArabicDigitsLocal = map[rune]rune{
	'٠': '0', '١': '1', '٢': '2', '٣': '3', '٤': '4',
	'٥': '5', '٦': '6', '٧': '7', '٨': '8', '٩': '9',
	'۰': '0', '۱': '1', '۲': '2', '۳': '3', '۴': '4',
	'۵': '5', '۶': '6', '۷': '7', '۸': '8', '۹': '9',
}

func normalizePhoneLocal(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	var b strings.Builder
	plus := false
	for _, r := range raw {
		if r == '+' {
			plus = true
			continue
		}
		if a, ok := easternArabicDigitsLocal[r]; ok {
			r = a
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
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
	if len(d) != 10 || d[0] != '7' {
		return "", false
	}
	return "+964" + d, true
}
