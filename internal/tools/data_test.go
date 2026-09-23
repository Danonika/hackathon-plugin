package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAnyTableTSVAndErrors(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "table.tsv"), "name\tvalue\na\t1\nb\t2\n")
	mustWrite(t, filepath.Join(root, "data", "input", "legacy.xls"), "old")
	mustWrite(t, filepath.Join(root, "data", "input", "empty.csv"), "")

	env := call(t, reg, "data.inspect", map[string]any{"path": "table.tsv"})
	if env.Status != "ok" {
		t.Fatalf("tsv status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["rows"].(int) != 2 || data["columns"].(int) != 2 {
		t.Fatalf("unexpected tsv shape %+v", data)
	}

	if env := call(t, reg, "data.inspect", map[string]any{"path": "legacy.xls"}); env.Status != "error" {
		t.Fatalf("xls must fail, got %+v", env)
	} else if !strings.Contains(env.Data.(map[string]any)["error"].(string), ".xls") {
		t.Fatalf("unexpected xls error %+v", env.Data)
	}
	if env := call(t, reg, "data.inspect", map[string]any{"path": "empty.csv"}); env.Status != "error" {
		t.Fatalf("empty csv must fail, got %+v", env)
	}
	if env := call(t, reg, "data.inspect", map[string]any{}); env.Status != "error" {
		t.Fatalf("missing path must fail, got %+v", env)
	}
	if env := call(t, reg, "data.inspect", map[string]any{"path": "missing.csv"}); env.Status != "error" {
		t.Fatalf("missing file must fail, got %+v", env)
	}
}

func TestBOMHeaderStripped(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "bom.csv"), "\ufeffts,value\n2026-01-01 00:00:00,1\n2026-01-01 01:00:00,2\n")

	env := call(t, reg, "files.describe", map[string]any{"path": "bom.csv"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	headers := data["headers"].([]string)
	if headers[0] != "ts" {
		t.Fatalf("headers[0] = %q, want ts", headers[0])
	}
	if data["has_timestamp"] != true {
		t.Fatalf("has_timestamp = %v, want true", data["has_timestamp"])
	}
}

func TestColumnIndexRejectsEmptyAndAmbiguous(t *testing.T) {
	tbl := &table{Headers: []string{"grid", "id_value", "value"}}
	if got := tbl.columnIndex(""); got != -1 {
		t.Fatalf("empty name = %d, want -1", got)
	}
	// Partial matches are refused: "id" must not bind to "id_value" or "grid".
	if got := tbl.columnIndex("id"); got != -1 {
		t.Fatalf("partial match = %d, want -1", got)
	}
	if got := tbl.columnIndex("value"); got != 2 {
		t.Fatalf("exact name = %d, want 2", got)
	}
	if got := tbl.columnIndex("id_value"); got != 1 {
		t.Fatalf("exact second name = %d, want 1", got)
	}
	ambiguous := &table{Headers: []string{"id_a", "id_b"}}
	if got := ambiguous.columnIndex("id"); got != -1 {
		t.Fatalf("ambiguous prefix = %d, want -1", got)
	}
}

func TestColumnIndexFallback(t *testing.T) {
	tbl := &table{Headers: []string{" Timestamp ", "Measured Value", "note"}}
	cases := []struct {
		name string
		want int
	}{
		{"timestamp", 0},
		{"measured value", 1},
		{"note", 2},
		{"absent", -1},
	}
	for _, c := range cases {
		if got := tbl.columnIndex(c.name); got != c.want {
			t.Fatalf("columnIndex(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestApplyWhereOperators(t *testing.T) {
	tbl := &table{
		Headers: []string{"name", "value"},
		Rows: [][]string{
			{"alfa", "10"},
			{"beta", "20"},
			{"gamma", ""},
		},
	}
	cases := []struct {
		where string
		want  int
	}{
		{"value = 10", 1},
		{"value == 20", 1},
		{"value != 10", 2},
		{"value > 10", 1},
		{"value >= 10", 2},
		{"value < 20", 1},
		{"value <= 20", 2},
		{"name contains AL", 1},
		{"name = ALFA", 1},
		{"value = ten", 0},
		{"name = delta", 0},
	}
	for _, c := range cases {
		got, _, err := applyWhere(tbl, c.where)
		if err != nil {
			t.Fatalf("applyWhere(%q): %v", c.where, err)
		}
		if len(got) != c.want {
			t.Fatalf("applyWhere(%q) matched %d, want %d", c.where, len(got), c.want)
		}
	}

	for _, bad := range []string{"value", "value >", "missing = 1", "value ~ 1"} {
		if _, _, err := applyWhere(tbl, bad); err == nil {
			t.Fatalf("applyWhere(%q) must fail", bad)
		}
	}
}

func TestDataQueryProjectionLimitAndErrors(t *testing.T) {
	reg, root := testRegistry(t)
	var b strings.Builder
	b.WriteString("name,value\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "row%d,%d\n", i, i)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "many.csv"), b.String())

	env := call(t, reg, "data.query", map[string]any{
		"path":    "many.csv",
		"columns": []any{"value"},
		"limit":   float64(5),
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["matched_rows"].(int) != 30 || data["returned"].(int) != 5 {
		t.Fatalf("unexpected counts %+v", data)
	}
	rows := data["rows"].([]map[string]string)
	if len(rows) != 5 {
		t.Fatalf("rows = %d", len(rows))
	}
	for _, row := range rows {
		if len(row) != 1 {
			t.Fatalf("projection leaked columns: %+v", row)
		}
		if _, ok := row["value"]; !ok {
			t.Fatalf("missing value column: %+v", row)
		}
	}

	defaulted := call(t, reg, "data.query", map[string]any{"path": "many.csv", "limit": float64(1000)})
	if got := defaulted.Data.(map[string]any)["returned"].(int); got != 30 {
		t.Fatalf("oversized limit must clamp to the 200 maximum (30 rows here), got %d", got)
	}

	filtered := call(t, reg, "data.query", map[string]any{"path": "many.csv", "where": "value >= 28"})
	if got := filtered.Data.(map[string]any)["matched_rows"].(int); got != 2 {
		t.Fatalf("matched_rows = %d, want 2", got)
	}

	if env := call(t, reg, "data.query", map[string]any{"path": "many.csv", "columns": []any{"nope"}}); env.Status != "error" {
		t.Fatalf("unknown column must fail, got %+v", env)
	}
	if env := call(t, reg, "data.query", map[string]any{"path": "many.csv", "where": "value ~ 1"}); env.Status != "error" {
		t.Fatalf("bad operator must fail, got %+v", env)
	}
}

func TestDataInspectSubsetAndWideWarning(t *testing.T) {
	reg, root := testRegistry(t)
	headers := make([]string, 66)
	for i := range headers {
		headers[i] = fmt.Sprintf("col_%02d", i)
	}
	var b strings.Builder
	b.WriteString(strings.Join(headers, ",") + "\n")
	for i := range headers {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf("%d", i))
	}
	b.WriteString("\n")
	mustWrite(t, filepath.Join(root, "data", "input", "wide.csv"), b.String())

	env := call(t, reg, "data.inspect", map[string]any{"path": "wide.csv"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	if !strings.Contains(strings.Join(env.Warnings, "|"), "more than 64 columns") {
		t.Fatalf("expected wide warning, got %v", env.Warnings)
	}
	if got := env.Data.(map[string]any)["columns"].(int); got != 66 {
		t.Fatalf("columns = %d", got)
	}

	subset := call(t, reg, "data.inspect", map[string]any{"path": "wide.csv", "columns": []any{"col_01", "col_05"}})
	profiles := subset.Data.(map[string]any)["profile"].([]columnProfile)
	if len(profiles) != 2 || profiles[0].Name != "col_01" || profiles[1].Name != "col_05" {
		t.Fatalf("unexpected subset profiles %+v", profiles)
	}
	if profiles[0].Kind != "numeric" || profiles[0].Min == nil || *profiles[0].Min != 1 {
		t.Fatalf("unexpected numeric profile %+v", profiles[0])
	}
	if profiles[0].Std == nil || profiles[1].Samples == nil {
		t.Fatalf("expected stats and samples in profiles %+v", profiles)
	}
}

func TestDataAnalyzeInsufficientRows(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "three.csv"), "x,y\n1,2\n2,4\n3,6\n")
	env := call(t, reg, "data.analyze", map[string]any{"path": "three.csv"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if len(data["correlations"].([]map[string]any)) != 0 {
		t.Fatalf("fewer than 5 rows must not produce correlations: %+v", data)
	}
}

func TestProfileTimestampAndTextKinds(t *testing.T) {
	tbl := &table{
		Headers: []string{"when", "label", "mixed"},
		Rows: [][]string{
			{"2026-01-01", "alfa", "1"},
			{"2026-01-02", "beta", "two"},
			{"2026-01-03", "gamma", "3"},
			{"2026-01-04", "", "four"},
			{"2026-01-05", "delta", "5"},
		},
	}
	profiles := profileColumns(tbl, nil)
	if profiles[0].Kind != "timestamp" {
		t.Fatalf("when kind = %q, want timestamp", profiles[0].Kind)
	}
	if profiles[1].Kind != "text" || profiles[1].Missing != 1 {
		t.Fatalf("label profile = %+v", profiles[1])
	}
	if profiles[2].Kind != "text" {
		t.Fatalf("mixed kind = %q, want text", profiles[2].Kind)
	}
}
