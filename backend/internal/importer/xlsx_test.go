package importer

// Builds a minimal, real XLSX (a zip with the two OOXML parts this
// converter reads) in-memory, so the parser is proven against actual zip +
// XML bytes rather than only its own intermediate structs.

import (
	"archive/zip"
	"bytes"
	"testing"
)

func buildTestXLSX(t *testing.T, sharedStrings []string, sheetXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if len(sharedStrings) > 0 {
		var sb bytes.Buffer
		sb.WriteString(`<?xml version="1.0"?><sst xmlns="x">`)
		for _, s := range sharedStrings {
			sb.WriteString("<si><t>" + s + "</t></si>")
		}
		sb.WriteString(`</sst>`)
		w, err := zw.Create("xl/sharedStrings.xml")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(sb.Bytes()); err != nil {
			t.Fatal(err)
		}
	}

	w, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(sheetXML)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestXlsxToCSV_SharedStringsAndNumbers(t *testing.T) {
	// Shared strings: 0="username", 1="quota", 2="ali01", 3="sara_2".
	sheet := `<?xml version="1.0"?><worksheet xmlns="x"><sheetData>
	  <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
	  <row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>1000</v></c></row>
	  <row r="3"><c r="A3" t="s"><v>3</v></c><c r="B3"><v>2000</v></c></row>
	</sheetData></worksheet>`
	raw := buildTestXLSX(t, []string{"username", "quota", "ali01", "sara_2"}, sheet)

	res, err := xlsxToCSV(raw)
	if err != nil {
		t.Fatalf("xlsxToCSV: %v", err)
	}
	if len(res.Header) != 2 || res.Header[0] != "username" || res.Header[1] != "quota" {
		t.Fatalf("header = %v, want [username quota]", res.Header)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if res.Rows[0][0] != "ali01" || res.Rows[0][1] != "1000" {
		t.Errorf("row 0 = %v, want [ali01 1000]", res.Rows[0])
	}
	if res.Rows[1][0] != "sara_2" || res.Rows[1][1] != "2000" {
		t.Errorf("row 1 = %v, want [sara_2 2000]", res.Rows[1])
	}
}

func TestXlsxToCSV_SparseRowPadsColumns(t *testing.T) {
	// Row 2 has no cell in column C — a real Excel export omits trailing
	// empty cells; the parser must still align column A/B/(missing C).
	sheet := `<?xml version="1.0"?><worksheet xmlns="x"><sheetData>
	  <row r="1"><c r="A1" t="inlineStr"><is><t>a</t></is></c><c r="B1" t="inlineStr"><is><t>b</t></is></c><c r="C1" t="inlineStr"><is><t>c</t></is></c></row>
	  <row r="2"><c r="A2" t="inlineStr"><is><t>1</t></is></c><c r="C2" t="inlineStr"><is><t>3</t></is></c></row>
	</sheetData></worksheet>`
	raw := buildTestXLSX(t, nil, sheet)

	res, err := xlsxToCSV(raw)
	if err != nil {
		t.Fatalf("xlsxToCSV: %v", err)
	}
	if len(res.Rows) != 1 || len(res.Rows[0]) != 3 {
		t.Fatalf("rows = %+v, want one 3-column row", res.Rows)
	}
	if res.Rows[0][0] != "1" || res.Rows[0][1] != "" || res.Rows[0][2] != "3" {
		t.Errorf("row = %v, want [1 \"\" 3]", res.Rows[0])
	}
}

func TestXlsxToCSV_NotAZipFile(t *testing.T) {
	if _, err := xlsxToCSV([]byte("not a zip")); err == nil {
		t.Fatal("expected an error for non-zip input")
	}
}

func TestColumnIndex(t *testing.T) {
	cases := map[string]int{"A1": 0, "B1": 1, "Z1": 25, "AA1": 26, "AB12": 27}
	for ref, want := range cases {
		got, err := columnIndex(ref)
		if err != nil {
			t.Fatalf("columnIndex(%q): %v", ref, err)
		}
		if got != want {
			t.Errorf("columnIndex(%q) = %d, want %d", ref, got, want)
		}
	}
}
