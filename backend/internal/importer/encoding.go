package importer

// Encoding + delimiter sniffing (FR-6): SAS4 and similar legacy tools export
// either UTF-8 or Windows-1256 (CP1256, the common Arabic Windows codepage) —
// never announced by a header, so the file itself is the only signal.

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// utf8BOM is the 3-byte UTF-8 byte order mark some exporters (Excel) prepend.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// detectEncoding reports "utf-8" when raw is valid UTF-8 (after stripping an
// optional BOM), else "cp1256" — Windows-1256 accepts every byte value so it
// is always a valid fallback decode, making "not valid UTF-8" a reliable
// enough signal in practice for the two encodings this wizard supports.
func detectEncoding(raw []byte) string {
	if utf8.Valid(stripBOM(raw)) {
		return "utf-8"
	}
	return "cp1256"
}

func stripBOM(raw []byte) []byte {
	return bytes.TrimPrefix(raw, utf8BOM)
}

// decodeToUTF8 converts raw bytes in the given encoding to a UTF-8 string,
// BOM-stripped.
func decodeToUTF8(raw []byte, encoding string) (string, error) {
	if encoding == "cp1256" {
		out, err := charmap.Windows1256.NewDecoder().Bytes(raw)
		if err != nil {
			return "", err
		}
		return string(stripBOM(out)), nil
	}
	return string(stripBOM(raw)), nil
}

// detectDelimiter picks comma or semicolon by counting occurrences in the
// header line — semicolon-delimited exports are common from Excel on
// Arabic-locale Windows installs (comma is the decimal separator there).
func detectDelimiter(headerLine string) rune {
	if strings.Count(headerLine, ";") > strings.Count(headerLine, ",") {
		return ';'
	}
	return ','
}

// firstLine returns the text up to the first newline (CRLF-safe).
func firstLine(s string) string {
	i := strings.IndexAny(s, "\r\n")
	if i < 0 {
		return s
	}
	return s[:i]
}
