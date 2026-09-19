package tools

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/mcp"
	"github.com/ledongthuc/pdf"
)

func pdfText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	reader, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func isTextExt(ext string) bool {
	switch ext {
	case "txt", "md", "csv", "tsv", "json", "log", "yaml", "yml", "xml", "html", "htm", "srt", "vtt":
		return true
	}
	return false
}

func extractText(path string) (string, []string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "docx":
		text, err := docxText(path)
		return text, nil, err
	case "xlsx", "xls":
		sheets, warnings, err := readXLSXSheets(path)
		if err != nil {
			return "", warnings, err
		}
		var b strings.Builder
		for i, s := range sheets {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "## Sheet: %s\n", s.Name)
			b.WriteString(tableAsText(s.Table))
		}
		return strings.TrimSpace(b.String()), warnings, nil
	case "pdf":
		text, err := pdfText(path)
		if err != nil {
			return "", []string{"pdf text extraction failed: " + err.Error()}, nil
		}
		if strings.TrimSpace(text) == "" {
			return "", []string{"pdf contains no extractable text (possibly a scanned document); OCR is required"}, nil
		}
		return text, nil, nil
	default:
		b, err := os.ReadFile(path)
		if err != nil {
			return "", nil, err
		}
		warnings := []string{}
		if !isTextExt(ext) {
			warnings = append(warnings, fmt.Sprintf("file type %q has no dedicated processor; raw content returned as text", ext))
		}
		return string(b), warnings, nil
	}
}

func tableAsText(t *table) string {
	var b strings.Builder
	b.WriteString(strings.Join(t.Headers, "\t"))
	b.WriteString("\n")
	limit := len(t.Rows)
	if limit > 500 {
		limit = 500
	}
	for _, row := range t.Rows[:limit] {
		b.WriteString(strings.Join(row, "\t"))
		b.WriteString("\n")
	}
	if len(t.Rows) > limit {
		fmt.Fprintf(&b, "... %d more rows omitted\n", len(t.Rows)-limit)
	}
	return b.String()
}

func docxText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("docx: word/document.xml not found")
	}
	rc, err := doc.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			switch t.Name.Local {
			case "tab":
				b.WriteString("\t")
			case "br":
				b.WriteString("\n")
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func registerDocuments(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"documents.ingest",
		"Extract text from an input file and store it as a reusable artifact. Supports txt, md, csv, tsv, json, log, docx, xlsx and text-like formats.",
		map[string]any{
			"path": strProp("Relative path under data/input"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			path := argString(args, "path")
			if path == "" {
				return mcp.Fail(trace(ctx), "path is required"), nil
			}
			full, err := d.Store.InputPath(path)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			text, warnings, err := extractText(full)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			stamp := time.Now().UTC().Format("20060102-150405")
			artifact, werr := d.Store.WriteArtifact(
				fmt.Sprintf("extracted/%s-%s-%s.txt", slugify(base), shortHash(path), stamp),
				[]byte(text),
			)
			if werr != nil {
				return mcp.Fail(trace(ctx), werr.Error()), nil
			}
			meta := fmt.Sprintf(`{"source":%q,"artifact_id":%q,"chars":%d,"extracted_at":%q}`,
				path, artifact, len([]rune(text)), time.Now().UTC().Format(time.RFC3339))
			_, _ = d.Store.WriteArtifact(
				fmt.Sprintf("extracted/%s-%s-%s.meta.json", slugify(base), shortHash(path), stamp),
				[]byte(meta),
			)
			env := mcp.OK(trace(ctx), map[string]any{
				"artifact_id": artifact,
				"chars":       len([]rune(text)),
				"preview":     snippet(text, 400),
			}).WithSources(mcp.Source{ArtifactID: artifact, Block: "full-text"})
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))

	reg.Register(tool(
		"documents.extract",
		"Extract text from an input file without storing an artifact. Use documents.ingest when the result must be indexed or reused.",
		map[string]any{
			"path":      strProp("Relative path under data/input"),
			"max_chars": numProp("Maximum characters to return (default 60000)"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			path := argString(args, "path")
			if path == "" {
				return mcp.Fail(trace(ctx), "path is required"), nil
			}
			full, err := d.Store.InputPath(path)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			text, warnings, err := extractText(full)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			maxChars := argInt(args, "max_chars", d.Cfg.Config().Tools.MaxTextChars)
			truncated := false
			r := []rune(text)
			if len(r) > maxChars {
				text = string(r[:maxChars])
				truncated = true
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"text":      text,
				"chars":     len(r),
				"truncated": truncated,
			})
			if truncated {
				env = env.WithWarnings("text truncated; use documents.ingest and knowledge.index for large documents")
			}
			env = env.WithWarnings(warnings...)
			return env, nil
		},
	))

	reg.Register(tool(
		"documents.get",
		"Read a previously stored extracted-text artifact by artifact_id.",
		map[string]any{
			"artifact_id": strProp("Artifact id returned by documents.ingest"),
			"max_chars":   numProp("Maximum characters to return"),
		},
		[]string{"artifact_id"},
		func(ctx context.Context, args map[string]any) (any, error) {
			id := argString(args, "artifact_id")
			if id == "" {
				return mcp.Fail(trace(ctx), "artifact_id is required"), nil
			}
			raw, err := d.Store.ReadArtifact(id)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			text := string(raw)
			maxChars := argInt(args, "max_chars", d.Cfg.Config().Tools.MaxTextChars)
			truncated := false
			r := []rune(text)
			if len(r) > maxChars {
				text = string(r[:maxChars])
				truncated = true
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"artifact_id": id,
				"text":        text,
				"chars":       len(r),
				"truncated":   truncated,
			}).WithSources(mcp.Source{ArtifactID: id, Block: "full-text"})
			return env, nil
		},
	))
}
