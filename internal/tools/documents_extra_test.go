package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPdfWithoutExtractableTextWarns(t *testing.T) {
	reg, root := testRegistry(t)
	if err := os.WriteFile(filepath.Join(root, "data", "input", "scanned.pdf"), buildTestPDF(""), 0o644); err != nil {
		t.Fatal(err)
	}
	env := call(t, reg, "documents.extract", map[string]any{"path": "scanned.pdf"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	if !strings.Contains(strings.Join(env.Warnings, "|"), "no extractable text") {
		t.Fatalf("warnings = %v", env.Warnings)
	}
	if env.Data.(map[string]any)["text"] != "" {
		t.Fatalf("text = %q", env.Data.(map[string]any)["text"])
	}
}

func TestDocxMalformedXML(t *testing.T) {
	reg, root := testRegistry(t)
	docx := buildDocxFile(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p>`,
	})
	raw, err := os.ReadFile(docx)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "malformed.docx"), string(raw))
	if env := call(t, reg, "documents.extract", map[string]any{"path": "malformed.docx"}); env.Status != "error" {
		t.Fatalf("malformed docx must fail: %+v", env)
	}
}

func TestDocumentsIngestPropagatesWarnings(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "blob.bin"), "raw-binary-content")

	env := call(t, reg, "documents.ingest", map[string]any{"path": "blob.bin"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	if !strings.Contains(strings.Join(env.Warnings, "|"), "no dedicated processor") {
		t.Fatalf("warnings = %v", env.Warnings)
	}
	artifactID := env.Data.(map[string]any)["artifact_id"].(string)
	content, err := os.ReadFile(filepath.Join(root, "data", "artifacts", filepath.FromSlash(artifactID)))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "raw-binary-content" {
		t.Fatalf("artifact content = %q", content)
	}
}
