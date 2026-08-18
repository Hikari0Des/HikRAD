package importer

// Minimal XLSX → CSV conversion (v4-5, FR-138: Pro Radius exports XLSX).
// XLSX is a zip of OOXML parts; this reads only what a plain data export
// needs — the first worksheet's cell values and the shared-string table —
// using nothing but the standard library (archive/zip, encoding/xml),
// deliberately avoiding a new third-party dependency for a single import
// source. No formulas, styles, merged cells, or multi-sheet workbooks are
// interpreted; a cell with a formula falls back to its cached value
// (Excel/most exporters always write one), and an unreadable file is
// reported as a normal upload validation error, never a panic.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

var errNoWorksheet = errors.New("importer: no worksheet found in this XLSX file")

type sharedStringsXML struct {
	Items []struct {
		Text string `xml:"t"`
		Runs []struct {
			Text string `xml:"t"`
		} `xml:"r"`
	} `xml:"si"`
}

// text joins a shared-string item's own <t> (a plain string) with any rich-
// text <r><t> runs (a formatted string) — an item has exactly one shape or
// the other, never both, so concatenating is safe.
func (s sharedStringsXML) text(i int) string {
	if i < 0 || i >= len(s.Items) {
		return ""
	}
	item := s.Items[i]
	if item.Text != "" || len(item.Runs) == 0 {
		return item.Text
	}
	var sb strings.Builder
	for _, r := range item.Runs {
		sb.WriteString(r.Text)
	}
	return sb.String()
}

type sheetXML struct {
	Rows []struct {
		Cells []struct {
			Ref   string `xml:"r,attr"`
			Type  string `xml:"t,attr"`
			Value string `xml:"v"`
			Inline struct {
				Text string `xml:"t"`
			} `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

// xlsxToCSV extracts the first worksheet found in a .xlsx archive as
// header+rows (the first row is the header, matching every other upload
// path). Cells are read by column LETTER from each <c r="A1">, so a sparse
// row (trailing empty cells Excel omits) still lands in the right column.
func xlsxToCSV(raw []byte) (dumpToCSVResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return dumpToCSVResult{}, fmt.Errorf("not a valid xlsx/zip file: %w", err)
	}

	var shared sharedStringsXML
	if f := findZipFile(zr, "xl/sharedStrings.xml"); f != nil {
		if err := decodeZipXML(f, &shared); err != nil {
			return dumpToCSVResult{}, fmt.Errorf("reading shared strings: %w", err)
		}
	}

	sheetFile := firstWorksheet(zr)
	if sheetFile == nil {
		return dumpToCSVResult{}, errNoWorksheet
	}
	var sheet sheetXML
	if err := decodeZipXML(sheetFile, &sheet); err != nil {
		return dumpToCSVResult{}, fmt.Errorf("reading worksheet: %w", err)
	}

	var grid [][]string
	maxCol := 0
	for _, row := range sheet.Rows {
		byCol := map[int]string{}
		for _, c := range row.Cells {
			col, err := columnIndex(c.Ref)
			if err != nil {
				continue
			}
			if col > maxCol {
				maxCol = col
			}
			switch c.Type {
			case "s":
				idx, _ := strconv.Atoi(strings.TrimSpace(c.Value))
				byCol[col] = shared.text(idx)
			case "inlineStr":
				byCol[col] = c.Inline.Text
			case "b":
				byCol[col] = map[string]string{"1": "TRUE", "0": "FALSE"}[c.Value]
			default: // number, or a formula's cached string/numeric result
				byCol[col] = c.Value
			}
		}
		line := make([]string, maxCol+1)
		for i := range line {
			line[i] = byCol[i]
		}
		grid = append(grid, line)
	}
	if len(grid) == 0 {
		return dumpToCSVResult{}, errNoWorksheet
	}
	// Every row must be the same width as the widest row seen — earlier rows
	// built before maxCol grew need padding.
	for i, row := range grid {
		if len(row) < maxCol+1 {
			padded := make([]string, maxCol+1)
			copy(padded, row)
			grid[i] = padded
		}
	}
	return dumpToCSVResult{Header: grid[0], Rows: grid[1:]}, nil
}

func findZipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// firstWorksheet picks xl/worksheets/sheetN.xml with the LOWEST N — the
// common single-sheet export case this converter targets; a workbook with
// several sheets would need workbook.xml's own sheet-order/visibility to
// pick correctly, which is out of scope for a plain data-export path.
func firstWorksheet(zr *zip.Reader) *zip.File {
	var candidates []*zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			candidates = append(candidates, f)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	return candidates[0]
}

func decodeZipXML(f *zip.File, dst any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	return xml.Unmarshal(body, dst)
}

// columnIndex converts a cell reference like "AB12" to a zero-based column
// index (A=0, B=1, ..., Z=25, AA=26, ...).
func columnIndex(ref string) (int, error) {
	col := 0
	found := false
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z':
			col = col*26 + int(r-'A'+1)
			found = true
		case r >= 'a' && r <= 'z':
			col = col*26 + int(r-'a'+1)
			found = true
		default:
			// digits (the row number) end the column part
			if found {
				return col - 1, nil
			}
			return 0, fmt.Errorf("invalid cell reference %q", ref)
		}
	}
	if !found {
		return 0, fmt.Errorf("invalid cell reference %q", ref)
	}
	return col - 1, nil
}

// xlsxRowsToCSVText is the convenience entry point upload() calls: parse the
// workbook, then render the SAME canonical CSV text every other source
// format converts to (writeCSV, shared with sqldump.go's dump converter).
func xlsxRowsToCSVText(raw []byte) (string, []string, error) {
	res, err := xlsxToCSV(raw)
	if err != nil {
		return "", nil, err
	}
	text, err := writeCSV(res)
	if err != nil {
		return "", nil, err
	}
	return text, res.Header, nil
}
