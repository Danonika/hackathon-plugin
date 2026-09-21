package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesDescribeTextFormats(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "table.csv"),
		"ts_value,value\n2026-01-01 00:00:00,1\n2026-01-01 01:00:00,2\n")
	mustWrite(t, filepath.Join(root, "data", "input", "table.tsv"), "name\tvalue\na\t1\nb\t2\n")
	mustWrite(t, filepath.Join(root, "data", "input", "notes.md"), "line one\nline two\n")
	mustWrite(t, filepath.Join(root, "data", "input", "blob.bin"), "\x00\x01binary")
	mustWrite(t, filepath.Join(root, "data", "input", "legacy.xls"), "old-biff")

	cases := []struct {
		name     string
		path     string
		status   string
		rows     int
		cols     int
		hasTS    bool
		warnPart string
	}{
		{name: "csv", path: "table.csv", status: "ok", rows: 2, cols: 2, hasTS: true},
		{name: "tsv", path: "table.tsv", status: "ok", rows: 2, cols: 2, hasTS: false},
		{name: "markdown", path: "notes.md", status: "ok"},
		{name: "binary", path: "blob.bin", status: "ok", warnPart: "binary or unsupported"},
		{name: "legacy xls", path: "legacy.xls", status: "ok", warnPart: "legacy .xls"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := call(t, reg, "files.describe", map[string]any{"path": c.path})
			if env.Status != c.status {
				t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
			}
			data := env.Data.(map[string]any)
			if c.rows > 0 {
				if got := data["rows"].(int); got != c.rows {
					t.Fatalf("rows = %d, want %d", got, c.rows)
				}
				if got := data["columns"].(int); got != c.cols {
					t.Fatalf("columns = %d, want %d", got, c.cols)
				}
				if got := data["has_timestamp"].(bool); got != c.hasTS {
					t.Fatalf("has_timestamp = %v, want %v", got, c.hasTS)
				}
			}
			if c.path == "notes.md" {
				if got := data["lines"].(int); got != 3 {
					t.Fatalf("lines = %d, want 3", got)
				}
				if got := data["chars"].(int); got != len("line one\nline two\n") {
					t.Fatalf("chars = %d", got)
				}
			}
			if c.warnPart != "" {
				joined := strings.Join(env.Warnings, "|")
				if !strings.Contains(joined, c.warnPart) {
					t.Fatalf("warnings %v missing %q", env.Warnings, c.warnPart)
				}
			}
		})
	}
}

func TestFilesDescribeXLSXSheets(t *testing.T) {
	reg, root := testRegistry(t)
	src := buildXLSX(t, []testSheet{
		{Name: "Telemetry", Rows: [][]string{{"ts_value", "value"}, {"2026-01-01 00:00:00", "1"}}},
		{Name: "Summary", Rows: [][]string{{"metric", "mean"}, {"value", "1"}}},
	})
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "book.xlsx"), string(raw))

	env := call(t, reg, "files.describe", map[string]any{"path": "book.xlsx"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	sheets, ok := data["sheets"].([]string)
	if !ok || len(sheets) != 2 || sheets[0] != "Telemetry" || sheets[1] != "Summary" {
		t.Fatalf("sheets = %#v", data["sheets"])
	}
	if data["first_sheet"] != "Telemetry" {
		t.Fatalf("first_sheet = %v", data["first_sheet"])
	}
	if data["rows"].(int) != 1 || data["columns"].(int) != 2 {
		t.Fatalf("unexpected shape %+v", data)
	}
	if data["has_timestamp"].(bool) != true {
		t.Fatalf("expected timestamp header detection, got %+v", data)
	}
}

func TestFilesDescribeErrorsAndWarnings(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "empty.csv"), "")

	env := call(t, reg, "files.describe", map[string]any{"path": "empty.csv"})
	if env.Status != "ok" {
		t.Fatalf("empty csv status %s", env.Status)
	}
	if !strings.Contains(strings.Join(env.Warnings, "|"), "empty table") {
		t.Fatalf("expected empty table warning, got %v", env.Warnings)
	}

	if env := call(t, reg, "files.describe", map[string]any{}); env.Status != "error" {
		t.Fatalf("missing path must fail, got %+v", env)
	}
	if env := call(t, reg, "files.describe", map[string]any{"path": "nope.csv"}); env.Status != "error" {
		t.Fatalf("missing file must fail, got %+v", env)
	}
	if env := call(t, reg, "files.describe", map[string]any{"path": "../../etc/passwd"}); env.Status != "error" {
		t.Fatalf("traversal must fail, got %+v", env)
	}
}

func TestTimestampHeaderKeywords(t *testing.T) {
	cases := map[string]bool{
		"time":              true,
		"event_date":        true,
		"Timestamp":         true,
		"ts_value":          true,
		"ts":                true,
		"ts_utc":            true,
		"event_ts":          true,
		"Дата":              true,
		"Время события":     true,
		"период":            true,
		"value":             false,
		"name":              false,
		"batch_counter":     false,
		"temperature_readi": false,
		"cats_x":            false,
		"results":           false,
	}
	for header, want := range cases {
		if got := timestampHeader(header); got != want {
			t.Fatalf("timestampHeader(%q) = %v, want %v", header, got, want)
		}
	}
}
