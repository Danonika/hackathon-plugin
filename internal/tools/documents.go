package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
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

const maxExtractedChars = 10_000_000
const maxRawTextBytes = 20 << 20

func pdfText(path string) (text string, truncated bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			truncated = false
			err = fmt.Errorf("pdf parse failed: %v", r)
		}
	}()
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	reader, err := r.GetPlainText()
	if err != nil {
		return "", false, err
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxExtractedChars+1))
	if err != nil {
		return "", false, err
	}
	if len(raw) > maxExtractedChars {
		raw = raw[:maxExtractedChars]
		truncated = true
	}
	return strings.TrimSpace(trimPartialRune(string(raw))), truncated, nil
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
	case "xlsx":
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
	case "xls":
		return "", nil, fmt.Errorf("legacy .xls (BIFF) workbooks are not supported; convert the file to .xlsx or .csv first")
	case "pdf":
		text, truncated, err := pdfText(path)
		if err != nil {
			return "", nil, fmt.Errorf("pdf text extraction failed: %w", err)
		}
		warnings := []string{}
		if truncated {
			warnings = append(warnings, fmt.Sprintf("pdf text truncated at %d bytes", maxExtractedChars))
		}
		if strings.TrimSpace(text) == "" {
			return "", append(warnings, "pdf contains no extractable text (possibly a scanned document); OCR is required"), nil
		}
		return text, warnings, nil
	default:
		st, statErr := os.Stat(path)
		if statErr != nil {
			return "", nil, statErr
		}
		warnings := []string{}
		limit := int64(maxRawTextBytes)
		if st.Size() > limit {
			warnings = append(warnings, fmt.Sprintf("file is larger than %dMB; only the first %dMB was read", maxRawTextBytes>>20, maxRawTextBytes>>20))
		}
		f, err := os.Open(path)
		if err != nil {
			return "", warnings, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, limit))
		if err != nil {
			return "", warnings, err
		}
		if !isTextExt(ext) {
			warnings = append(warnings, fmt.Sprintf("file type %q has no dedicated processor; raw content returned as text", ext))
		}
		return trimPartialRune(string(b)), warnings, nil
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

const maxDocxXMLBytes = 64 << 20

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
	limited := io.LimitReader(rc, maxDocxXMLBytes+1)
	dec := xml.NewDecoder(limited)
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
		"Extract text from an input file and store it as a reusable artifact. Supports txt, md, csv, tsv, json, log, docx, pdf, xlsx and text-like formats. XLSX input yields every worksheet; a scanned PDF returns empty text plus a warning (OCR required).",
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
			metaBytes, merr := json.Marshal(map[string]any{
				"source":       path,
				"artifact_id":  artifact,
				"chars":        len([]rune(text)),
				"extracted_at": time.Now().UTC().Format(time.RFC3339),
			})
			if merr != nil {
				return mcp.Fail(trace(ctx), merr.Error()), nil
			}
			meta := string(metaBytes)
			if _, werr := d.Store.WriteArtifact(
				fmt.Sprintf("extracted/%s-%s-%s.meta.json", slugify(base), shortHash(path), stamp),
				[]byte(meta),
			); werr != nil {
				warnings = append(warnings, "metadata artifact not stored: "+werr.Error())
			}
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
			if maxChars <= 0 || maxChars > 10_000_000 {
				maxChars = d.Cfg.Config().Tools.MaxTextChars
			}
			if maxChars > 10_000_000 {
				maxChars = 10_000_000
			}
			if maxChars <= 0 {
				maxChars = 60000
			}
			truncated := false
			r := []rune(text)
			if len(r) > maxChars {
				text = string(r[:maxChars])
				truncated = true
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"text":        text,
				"chars":       len([]rune(text)),
				"total_chars": len(r),
				"truncated":   truncated,
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
			"offset":      numProp("Character offset to start from (default 0)"),
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
			if maxChars <= 0 || maxChars > 10_000_000 {
				maxChars = d.Cfg.Config().Tools.MaxTextChars
			}
			if maxChars <= 0 {
				maxChars = 60000
			}
			offset := argInt(args, "offset", 0)
			if offset < 0 {
				offset = 0
			}
			truncated := false
			r := []rune(text)
			if offset > len(r) {
				offset = len(r)
			}
			remaining := r[offset:]
			if len(remaining) > maxChars {
				remaining = remaining[:maxChars]
				truncated = true
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"artifact_id": id,
				"text":        string(remaining),
				"offset":      offset,
				"chars":       len(remaining),
				"total_chars": len(r),
				"truncated":   truncated,
			}).WithSources(mcp.Source{ArtifactID: id, Block: "full-text"})
			return env, nil
		},
	))
}
