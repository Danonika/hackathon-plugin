package tools

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildDocxFile(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.docx")
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

func TestDocumentsIngestAndGet(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "notes.txt"), "abcdefghij")

	ingest := call(t, reg, "documents.ingest", map[string]any{"path": "notes.txt"})
	if ingest.Status != "ok" {
		t.Fatalf("ingest status %s data %+v warnings %v", ingest.Status, ingest.Data, ingest.Warnings)
	}
	data := ingest.Data.(map[string]any)
	if data["chars"].(int) != 10 || data["preview"] != "abcdefghij" {
		t.Fatalf("unexpected ingest data %+v", data)
	}
	artifactID := data["artifact_id"].(string)
	if !strings.HasPrefix(artifactID, "extracted/notes-") || !strings.HasSuffix(artifactID, ".txt") {
		t.Fatalf("artifact id %q", artifactID)
	}
	if len(ingest.Sources) == 0 || ingest.Sources[0].ArtifactID != artifactID {
		t.Fatalf("ingest sources %+v", ingest.Sources)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "artifacts", filepath.FromSlash(artifactID))); err != nil {
		t.Fatalf("artifact missing: %v", err)
	}

	truncated := call(t, reg, "documents.get", map[string]any{"artifact_id": artifactID, "max_chars": float64(4)})
	if truncated.Status != "ok" {
		t.Fatalf("get status %s data %+v", truncated.Status, truncated.Data)
	}
	td := truncated.Data.(map[string]any)
	if td["text"] != "abcd" || td["truncated"] != true || td["total_chars"].(int) != 10 || td["offset"].(int) != 0 {
		t.Fatalf("truncated get data %+v", td)
	}

	offset := call(t, reg, "documents.get", map[string]any{"artifact_id": artifactID, "offset": float64(8), "max_chars": float64(4)})
	if od := offset.Data.(map[string]any); od["text"] != "ij" || od["offset"].(int) != 8 || od["truncated"] != false {
		t.Fatalf("offset get data %+v", od)
	}

	beyond := call(t, reg, "documents.get", map[string]any{"artifact_id": artifactID, "offset": float64(99)})
	if bd := beyond.Data.(map[string]any); bd["chars"].(int) != 0 || bd["offset"].(int) != 10 {
		t.Fatalf("offset beyond end data %+v", bd)
	}

	negative := call(t, reg, "documents.get", map[string]any{"artifact_id": artifactID, "offset": float64(-3), "max_chars": float64(0)})
	if nd := negative.Data.(map[string]any); nd["offset"].(int) != 0 || nd["text"] != "abcdefghij" {
		t.Fatalf("negative offset data %+v", nd)
	}

	if env := call(t, reg, "documents.get", map[string]any{}); env.Status != "error" {
		t.Fatalf("missing artifact_id must fail: %+v", env)
	}
	if env := call(t, reg, "documents.get", map[string]any{"artifact_id": "extracted/missing.txt"}); env.Status != "error" {
		t.Fatalf("missing artifact must fail: %+v", env)
	}
}

func TestDocumentsIngestDocx(t *testing.T) {
	reg, root := testRegistry(t)
	docx := buildDocxFile(t, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:tab/><w:t>Tabbed</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br/><w:t>Next</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})
	raw, err := os.ReadFile(docx)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "letter.docx"), string(raw))

	ingest := call(t, reg, "documents.ingest", map[string]any{"path": "letter.docx"})
	if ingest.Status != "ok" {
		t.Fatalf("docx status %s data %+v warnings %v", ingest.Status, ingest.Data, ingest.Warnings)
	}
	artifactID := ingest.Data.(map[string]any)["artifact_id"].(string)
	content, err := os.ReadFile(filepath.Join(root, "data", "artifacts", filepath.FromSlash(artifactID)))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "Hello\tTabbed\n\nNext" {
		t.Fatalf("docx text = %q", got)
	}
}

func TestDocumentsIngestXLSXMultiSheet(t *testing.T) {
	reg, root := testRegistry(t)
	rows := [][]string{{"ts_value", "value"}}
	for i := 0; i < 505; i++ {
		rows = append(rows, []string{fmt.Sprintf("2026-01-01 00:00:%02d", i%60), fmt.Sprintf("%d", i)})
	}
	src := buildXLSX(t, []testSheet{
		{Name: "Data", Rows: rows},
		{Name: "Empty", Rows: nil},
	})
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "book.xlsx"), string(raw))

	ingest := call(t, reg, "documents.ingest", map[string]any{"path": "book.xlsx"})
	if ingest.Status != "ok" {
		t.Fatalf("xlsx status %s data %+v warnings %v", ingest.Status, ingest.Data, ingest.Warnings)
	}
	artifactID := ingest.Data.(map[string]any)["artifact_id"].(string)
	content, err := os.ReadFile(filepath.Join(root, "data", "artifacts", filepath.FromSlash(artifactID)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "## Sheet: Data") || !strings.Contains(text, "## Sheet: Empty") {
		t.Fatalf("missing sheet headers: %.200s", text)
	}
	if !strings.Contains(text, "ts_value\tvalue") {
		t.Fatalf("missing tab-joined headers: %.200s", text)
	}
	if !strings.Contains(text, "... 5 more rows omitted") {
		t.Fatalf("expected row omission note: %.300s", text)
	}
}

func TestDocumentsExtractTruncationAndWarnings(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "long.txt"), strings.Repeat("a", 100))
	mustWrite(t, filepath.Join(root, "data", "input", "blob.bin"), "binary-ish")
	mustWrite(t, filepath.Join(root, "data", "input", "legacy.xls"), "biff")

	short := call(t, reg, "documents.extract", map[string]any{"path": "long.txt", "max_chars": float64(10)})
	if short.Status != "ok" {
		t.Fatalf("status %s data %+v", short.Status, short.Data)
	}
	sd := short.Data.(map[string]any)
	if sd["text"] != strings.Repeat("a", 10) || sd["truncated"] != true || sd["chars"].(int) != 100 {
		t.Fatalf("unexpected truncation data %+v", sd)
	}
	if len(short.Warnings) == 0 {
		t.Fatalf("expected truncation warning, got %+v", short)
	}

	defaulted := call(t, reg, "documents.extract", map[string]any{"path": "long.txt", "max_chars": float64(-1)})
	if dd := defaulted.Data.(map[string]any); dd["truncated"] != false || dd["chars"].(int) != 100 {
		t.Fatalf("negative max_chars data %+v", dd)
	}
	oversized := call(t, reg, "documents.extract", map[string]any{"path": "long.txt", "max_chars": float64(20_000_000)})
	if od := oversized.Data.(map[string]any); od["truncated"] != false {
		t.Fatalf("out-of-range max_chars data %+v", od)
	}

	binary := call(t, reg, "documents.extract", map[string]any{"path": "blob.bin"})
	if binary.Status != "ok" {
		t.Fatalf("binary status %s", binary.Status)
	}
	if !strings.Contains(strings.Join(binary.Warnings, "|"), "no dedicated processor") {
		t.Fatalf("expected unsupported-type warning, got %v", binary.Warnings)
	}

	if env := call(t, reg, "documents.extract", map[string]any{"path": "legacy.xls"}); env.Status != "error" {
		t.Fatalf("xls must fail: %+v", env)
	}
	if env := call(t, reg, "documents.extract", map[string]any{"path": "missing.txt"}); env.Status != "error" {
		t.Fatalf("missing file must fail: %+v", env)
	}
	if env := call(t, reg, "documents.extract", map[string]any{}); env.Status != "error" {
		t.Fatalf("missing path must fail: %+v", env)
	}
	if env := call(t, reg, "documents.ingest", map[string]any{}); env.Status != "error" {
		t.Fatalf("ingest without path must fail: %+v", env)
	}
	if env := call(t, reg, "documents.ingest", map[string]any{"path": "missing.txt"}); env.Status != "error" {
		t.Fatalf("ingest missing file must fail: %+v", env)
	}
	if env := call(t, reg, "documents.ingest", map[string]any{"path": "legacy.xls"}); env.Status != "error" {
		t.Fatalf("ingest xls must fail: %+v", env)
	}
}

func TestDocumentsExtractMalformedInputs(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "broken.xlsx"), "this is not a zip")
	mustWrite(t, filepath.Join(root, "data", "input", "broken.docx"), "this is not a zip either")
	mustWrite(t, filepath.Join(root, "data", "input", "broken.pdf"), "this is not a pdf")

	if env := call(t, reg, "documents.extract", map[string]any{"path": "broken.xlsx"}); env.Status != "error" {
		t.Fatalf("bad xlsx must fail: %+v", env)
	}
	if env := call(t, reg, "documents.extract", map[string]any{"path": "broken.docx"}); env.Status != "error" {
		t.Fatalf("bad docx must fail: %+v", env)
	}

	pdf := call(t, reg, "documents.extract", map[string]any{"path": "broken.pdf"})
	if pdf.Status != "ok" {
		t.Fatalf("malformed pdf status %s data %+v", pdf.Status, pdf.Data)
	}
	if !strings.Contains(strings.Join(pdf.Warnings, "|"), "pdf text extraction failed") {
		t.Fatalf("expected pdf warning, got %v", pdf.Warnings)
	}
}

func TestDocxTextErrors(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain.docx")
	if err := os.WriteFile(plain, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := docxText(plain); err == nil {
		t.Fatal("non-zip docx must fail")
	}
	noDoc := buildDocxFile(t, map[string]string{"word/other.xml": "<xml/>"})
	if _, err := docxText(noDoc); err == nil || !strings.Contains(err.Error(), "word/document.xml") {
		t.Fatalf("docx without document.xml error = %v", err)
	}
}

func TestExtractTextLargeFileWarning(t *testing.T) {
	reg, root := testRegistry(t)
	bigPath := filepath.Join(root, "data", "input", "big.log")
	if err := os.MkdirAll(filepath.Dir(bigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(21 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	env := call(t, reg, "documents.extract", map[string]any{"path": "big.log", "max_chars": float64(10)})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	if !strings.Contains(strings.Join(env.Warnings, "|"), "larger than 20MB") {
		t.Fatalf("expected size warning, got %v", env.Warnings)
	}
}

func TestTableAsTextLimit(t *testing.T) {
	rows := make([][]string, 0, 505)
	for i := 0; i < 505; i++ {
		rows = append(rows, []string{fmt.Sprintf("r%d", i), fmt.Sprintf("%d", i)})
	}
	tbl := &table{Headers: []string{"name", "value"}, Rows: rows}
	text := tableAsText(tbl)
	if !strings.Contains(text, "... 5 more rows omitted") {
		t.Fatalf("expected omission note: %.120s", text)
	}
	small := tableAsText(&table{Headers: []string{"a"}, Rows: [][]string{{"1"}, {"2"}}})
	if strings.Contains(small, "omitted") {
		t.Fatalf("small table must not be truncated: %q", small)
	}
	if empty := tableAsText(&table{}); empty != "\n" {
		t.Fatalf("empty table text = %q", empty)
	}
}
