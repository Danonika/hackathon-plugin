package tools

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type testSheet struct {
	Name string
	Rows [][]string
}

func buildXLSX(t *testing.T, sheets []testSheet) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.xlsx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	writeFile := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	index := map[string]int{}
	values := []string{}
	getIdx := func(v string) int {
		if i, ok := index[v]; ok {
			return i
		}
		index[v] = len(values)
		values = append(values, v)
		return index[v]
	}

	sheetXMLs := make([]string, len(sheets))
	for si, sh := range sheets {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
		for ri, row := range sh.Rows {
			fmt.Fprintf(&b, `<row r="%d">`, ri+1)
			for ci, cell := range row {
				ref := colName(ci) + strconv.Itoa(ri+1)
				fmt.Fprintf(&b, `<c r="%s" t="s"><v>%d</v></c>`, ref, getIdx(cell))
			}
			b.WriteString(`</row>`)
		}
		b.WriteString(`</sheetData></worksheet>`)
		sheetXMLs[si] = b.String()
	}

	var sst strings.Builder
	sst.WriteString(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, v := range values {
		sst.WriteString("<si><t>" + xmlEscape(v) + "</t></si>")
	}
	sst.WriteString(`</sst>`)
	writeFile("xl/sharedStrings.xml", sst.String())

	var wb, rels strings.Builder
	wb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, sh := range sheets {
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscape(sh.Name), i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
	}
	wb.WriteString(`</sheets></workbook>`)
	rels.WriteString(`</Relationships>`)
	writeFile("xl/workbook.xml", wb.String())
	writeFile("xl/_rels/workbook.xml.rels", rels.String())
	for i, s := range sheetXMLs {
		writeFile(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), s)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func colName(i int) string {
	i++
	name := ""
	for i > 0 {
		i--
		name = string(rune('A'+i%26)) + name
		i /= 26
	}
	return name
}

func TestXLSXAllSheets(t *testing.T) {
	path := buildXLSX(t, []testSheet{
		{Name: "Telemetry", Rows: [][]string{{"ts", "value"}, {"2026-01-01 00:00:00", "10"}}},
		{Name: "Summary", Rows: [][]string{{"metric", "mean"}, {"value", "10"}}},
	})
	sheets, warnings, err := readXLSXSheets(path)
	if err != nil {
		t.Fatalf("readXLSXSheets: %v (warnings %v)", err, warnings)
	}
	if len(sheets) != 2 {
		t.Fatalf("expected 2 sheets, got %d", len(sheets))
	}
	if sheets[0].Name != "Telemetry" || sheets[1].Name != "Summary" {
		t.Fatalf("unexpected sheet names: %q, %q", sheets[0].Name, sheets[1].Name)
	}
	if got := sheets[1].Table.Headers[0]; got != "metric" {
		t.Fatalf("unexpected header of second sheet: %q", got)
	}
}

func TestXLSXSheetSelection(t *testing.T) {
	path := buildXLSX(t, []testSheet{
		{Name: "First", Rows: [][]string{{"a"}, {"1"}}},
		{Name: "Second", Rows: [][]string{{"b"}, {"2"}}},
	})
	tbl, _, err := readXLSXTable(path, "Second")
	if err != nil {
		t.Fatal(err)
	}
	if tbl.Headers[0] != "b" {
		t.Fatalf("expected sheet Second, got header %q", tbl.Headers[0])
	}
	if _, _, err := readXLSXTable(path, "Missing"); err == nil {
		t.Fatal("expected error for a missing sheet")
	}
}

func buildTestPDF(text string) []byte {
	var buf bytes.Buffer
	offsets := []int{}
	write := func(s string) { buf.WriteString(s) }
	addObj := func(n int, body string) {
		offsets = append(offsets, buf.Len())
		write(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", n, body))
	}
	write("%PDF-1.4\n")
	content := fmt.Sprintf("BT /F1 18 Tf 20 100 Td (%s) Tj ET", text)
	addObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	addObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	addObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 144] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>")
	addObj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	addObj(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	xrefPos := buf.Len()
	write(fmt.Sprintf("xref\n0 %d\n", len(offsets)+1))
	write("0000000000 65535 f \n")
	for _, off := range offsets {
		write(fmt.Sprintf("%010d 00000 n \n", off))
	}
	write(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xrefPos))
	return buf.Bytes()
}

func TestPDFTextExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, buildTestPDF("Hello Hackathon Agent Kit PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, warnings, err := extractText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello Hackathon Agent Kit PDF") {
		t.Fatalf("unexpected pdf text %q (warnings %v)", text, warnings)
	}
}
