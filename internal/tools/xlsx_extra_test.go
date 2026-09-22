package tools

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildZipFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.xlsx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func inlineSheet(rows ...[]string) string {
	out := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`
	for ri, row := range rows {
		ref := strconv.Itoa(ri + 1)
		out += `<row r="` + ref + `">`
		for ci, value := range row {
			out += `<c r="` + colName(ci) + ref + `" t="inlineStr"><is><t>` + xmlEscape(value) + `</t></is></c>`
		}
		out += `</row>`
	}
	return out + `</sheetData></worksheet>`
}

func TestXLSXSheetFileFallbackOrdering(t *testing.T) {
	path := buildZipFiles(t, map[string]string{
		"xl/worksheets/sheet10.xml": inlineSheet([]string{"Beta"}),
		"xl/worksheets/sheet2.xml":  inlineSheet([]string{"Alpha"}),
		"xl/worksheets/sheetX.xml":  inlineSheet([]string{"Gamma"}),
	})
	sheets, warnings, err := readXLSXSheets(path)
	if err != nil {
		t.Fatalf("readXLSXSheets: %v (warnings %v)", err, warnings)
	}
	if len(sheets) != 3 {
		t.Fatalf("expected 3 sheets, got %d", len(sheets))
	}
	if sheets[0].Name != "Sheet1" || sheets[0].Table.Headers[0] != "Alpha" {
		t.Fatalf("first sheet = %q %+v", sheets[0].Name, sheets[0].Table.Headers)
	}
	if sheets[1].Name != "Sheet2" || sheets[1].Table.Headers[0] != "Beta" {
		t.Fatalf("second sheet = %q %+v", sheets[1].Name, sheets[1].Table.Headers)
	}
	if sheets[2].Table.Headers[0] != "Gamma" {
		t.Fatalf("unparseable sheet number must sort last: %+v", sheets[2].Table.Headers)
	}

	tbl, _, err := readXLSXTable(path, "Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if tbl.Headers[0] != "Alpha" {
		t.Fatalf("readXLSXTable fallback picked %+v", tbl.Headers)
	}
}

func TestXLSXMissingSheetAndNoWorksheets(t *testing.T) {
	const relsXML = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`
	const wbXML = `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Gone" sheetId="1" r:id="rId1"/></sheets></workbook>`

	missing := buildZipFiles(t, map[string]string{
		"xl/workbook.xml":            wbXML,
		"xl/_rels/workbook.xml.rels": relsXML,
	})
	sheets, warnings, err := readXLSXSheets(missing)
	if err != nil {
		t.Fatalf("missing sheet file must be a warning, got error %v", err)
	}
	if len(sheets) != 0 {
		t.Fatalf("expected no parsed sheets, got %d", len(sheets))
	}
	if len(warnings) == 0 || warnings[0] == "" {
		t.Fatalf("expected missing-sheet warning, got %v", warnings)
	}
	if _, _, err := readXLSXTable(missing, ""); err == nil {
		t.Fatal("readXLSXTable must fail when the sheet file is missing")
	}

	empty := buildZipFiles(t, map[string]string{
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets></sheets></workbook>`,
	})
	if _, _, err := readXLSXSheets(empty); err == nil {
		t.Fatal("readXLSXSheets must fail without worksheets")
	}
	if _, _, err := readXLSXTable(empty, ""); err == nil {
		t.Fatal("readXLSXTable must fail without worksheets")
	}

	badShared := buildZipFiles(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst><si><t>broken`,
		"xl/worksheets/sheet1.xml": inlineSheet([]string{"Kept"}),
	})
	sheets, warnings, err = readXLSXSheets(badShared)
	if err != nil {
		t.Fatalf("malformed sharedStrings must not fail: %v", err)
	}
	if len(sheets) != 1 {
		t.Fatalf("expected one sheet despite bad sharedStrings, got %d", len(sheets))
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "failed to read sharedStrings.xml") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sharedStrings warning, got %v", warnings)
	}
}

func TestXLSXInlineStringsAndRefFallbacks(t *testing.T) {
	sheet := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="1">` +
		`<c r="A1" t="inlineStr"><is><t>Inline</t></is></c>` +
		`<c r="B1" t="n"><v>1.5</v></c>` +
		`<c r="C1" t="s"><v>99</v></c>` +
		`<c t="inlineStr"><is><t>NoRef</t></is></c>` +
		`</row></sheetData></worksheet>`
	path := buildZipFiles(t, map[string]string{"xl/worksheets/sheet1.xml": sheet})
	tbl, _, err := readXLSXTable(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tbl.Headers) != 4 {
		t.Fatalf("headers = %+v", tbl.Headers)
	}
	if tbl.Headers[0] != "Inline" || tbl.Headers[1] != "1.5" || tbl.Headers[3] != "NoRef" {
		t.Fatalf("unexpected headers %+v", tbl.Headers)
	}
	if tbl.Headers[2] != "99" {
		t.Fatalf("out-of-range shared string must stay raw, got %q", tbl.Headers[2])
	}
}

func TestXLSXStylesAdvanced(t *testing.T) {
	const stylesXML = `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<numFmts count="2"><numFmt numFmtId="164" formatCode="yyyy-mm-dd"/><numFmt numFmtId="165" formatCode="[h]:mm"/></numFmts>` +
		`<cellXfs count="5"><xf numFmtId="14"/><xf numFmtId="18"/><xf numFmtId="45"/><xf numFmtId="164"/><xf numFmtId="165"/></cellXfs>` +
		`</styleSheet>`
	const wbXML = `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><workbookPr date1904="true"/><sheets><sheet name="Dated" sheetId="1" r:id="rId1"/></sheets></workbook>`
	const relsXML = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`
	sheet := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="1"><c r="A1" t="inlineStr"><is><t>label</t></is></c></row>` +
		`<row r="2">` +
		`<c r="A2" s="0"><v>1</v></c>` +
		`<c r="B2" s="1"><v>0.5</v></c>` +
		`<c r="C2" s="2"><v>1.5</v></c>` +
		`<c r="D2" s="3"><v>1</v></c>` +
		`<c r="E2" s="4"><v>0.25</v></c>` +
		`<c r="F2" s="99"><v>7</v></c>` +
		`</row></sheetData></worksheet>`

	path := buildZipFiles(t, map[string]string{
		"xl/styles.xml":              stylesXML,
		"xl/workbook.xml":            wbXML,
		"xl/_rels/workbook.xml.rels": relsXML,
		"xl/worksheets/sheet1.xml":   sheet,
	})
	sheets, warnings, err := readXLSXSheets(path)
	if err != nil {
		t.Fatalf("readXLSXSheets: %v (%v)", err, warnings)
	}
	if len(sheets) != 1 || len(sheets[0].Table.Rows) != 1 {
		t.Fatalf("unexpected sheets %+v", sheets)
	}
	row := sheets[0].Table.Rows[0]
	want := []string{"1904-01-02", "12:00:00", "36:00:00", "1904-01-02", "6:00:00", "7"}
	for i, w := range want {
		if i >= len(row) || row[i] != w {
			t.Fatalf("row[%d] = %q, want %q (row %+v)", i, row[i], w, row)
		}
	}
}

func TestExcelSerialEdgeCases(t *testing.T) {
	if got := excelSerialToTime(59, false).Format("2006-01-02"); got != "1900-02-28" {
		t.Fatalf("serial 59 = %s, want 1900-02-28", got)
	}
	if got := excelSerialToTime(0, true).Format("2006-01-02"); got != "1904-01-01" {
		t.Fatalf("1904 serial 0 = %s", got)
	}
	if got := formatExcelDateTime(excelSerialToTime(46023.5, false)); got != "2026-01-01 12:00:00" {
		t.Fatalf("datetime with time component = %q", got)
	}
	if got := formatExcelTime(-0.25); got != "18:00:00" {
		t.Fatalf("formatExcelTime(-0.25) = %q", got)
	}
	if got := formatExcelElapsed(-0.25); got != "-6:00:00" {
		t.Fatalf("formatExcelElapsed(-0.25) = %q", got)
	}
}

func TestColFromRefAndRowsToTable(t *testing.T) {
	cases := map[string]int{
		"":     -1,
		"123":  -1,
		"a1":   -1,
		"A1":   0,
		"Z1":   25,
		"AA1":  26,
		"AB12": 27,
	}
	for ref, want := range cases {
		if got := colFromRef(ref); got != want {
			t.Fatalf("colFromRef(%q) = %d, want %d", ref, got, want)
		}
	}

	if tbl := rowsToTable(nil); len(tbl.Headers) != 0 || len(tbl.Rows) != 0 {
		t.Fatalf("empty rowsToTable = %+v", tbl)
	}
	tbl := rowsToTable([][]string{{"", "b"}, {"1"}, {"2", "3", "4"}})
	if len(tbl.Headers) != 3 || tbl.Headers[0] != "column_1" || tbl.Headers[1] != "b" || tbl.Headers[2] != "column_3" {
		t.Fatalf("headers = %+v", tbl.Headers)
	}
	if len(tbl.Rows) != 2 || tbl.Rows[0][1] != "" || tbl.Rows[0][2] != "" || tbl.Rows[1][2] != "4" {
		t.Fatalf("padded rows = %+v", tbl.Rows)
	}
}

func TestXLSXCellBeyondMaxColumn(t *testing.T) {
	sheet := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="1"><c r="ZZZZZ1" t="inlineStr"><is><t>far</t></is></c><c r="A1" t="inlineStr"><is><t>near</t></is></c></row>` +
		`</sheetData></worksheet>`
	path := buildZipFiles(t, map[string]string{"xl/worksheets/sheet1.xml": sheet})
	tbl, _, err := readXLSXTable(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tbl.Headers) != 1 || tbl.Headers[0] != "near" {
		t.Fatalf("out-of-range column must be dropped: %+v", tbl.Headers)
	}
}

func TestWorkbookSheetsEdgeCases(t *testing.T) {
	const wbXML = `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
		`<sheet name="" sheetId="1" r:id="rId1"/>` +
		`<sheet name="NoRid" sheetId="2"/>` +
		`<sheet name="MissingRel" sheetId="3" r:id="rId9"/>` +
		`<sheet name="Second" sheetId="4" r:id="rId2"/>` +
		`</sheets></workbook>`
	const relsXML = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="/xl/worksheets/sheet1.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>` +
		`</Relationships>`
	path := buildZipFiles(t, map[string]string{
		"xl/workbook.xml":            wbXML,
		"xl/_rels/workbook.xml.rels": relsXML,
	})
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	refs, _ := workbookSheets(zr)
	if len(refs) != 2 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Name != "Sheet1" || refs[0].Path != "xl/worksheets/sheet1.xml" {
		t.Fatalf("first ref = %+v", refs[0])
	}
	if refs[1].Name != "Second" || refs[1].Path != "xl/worksheets/sheet2.xml" {
		t.Fatalf("second ref = %+v", refs[1])
	}
}

func TestNormalizeTarget(t *testing.T) {
	cases := map[string]string{
		"/xl/worksheets/sheet1.xml": "xl/worksheets/sheet1.xml",
		"worksheets/sheet1.xml":     "xl/worksheets/sheet1.xml",
		"xl/worksheets/sheet1.xml":  "xl/worksheets/sheet1.xml",
	}
	for in, want := range cases {
		if got := normalizeTarget(in); got != want {
			t.Fatalf("normalizeTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuiltinFormatKindRanges(t *testing.T) {
	if got := builtinFormatKind(50); got != (fmtKind{date: true}) {
		t.Fatalf("builtin 50 = %+v", got)
	}
	if got := builtinFormatKind(46); got != (fmtKind{elapsed: true}) {
		t.Fatalf("builtin 46 = %+v", got)
	}
	if got := builtinFormatKind(1); got != (fmtKind{}) {
		t.Fatalf("builtin 1 = %+v", got)
	}
	if !isElapsedToken(" hms ") || isElapsedToken("") || isElapsedToken("hmx") {
		t.Fatal("isElapsedToken edge cases")
	}
	if got := formatExcelDateTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); got != "2026-01-01" {
		t.Fatalf("formatExcelDateTime = %q", got)
	}
}
