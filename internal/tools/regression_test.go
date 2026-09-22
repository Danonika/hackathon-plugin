package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestInvalidArgumentsDoNotPanic(t *testing.T) {
	reg, root := testRegistry(t)
	var b strings.Builder
	b.WriteString("ts,value\n")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&b, "2026-01-01 0%d:00:00,%d\n", i, i*10)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "tiny.csv"), b.String())
	mustWrite(t, filepath.Join(root, "data", "input", "t.txt"), "hello")

	extract := call(t, reg, "documents.extract", map[string]any{"path": "t.txt", "max_chars": float64(-1)})
	if extract.Status != "ok" {
		t.Fatalf("extract with max_chars=-1: %+v", extract)
	}
	get := call(t, reg, "documents.get", map[string]any{"artifact_id": "extracted/missing.txt", "max_chars": float64(-1), "offset": float64(-5)})
	if get.Status != "error" {
		t.Fatalf("missing artifact must return an error envelope: %+v", get)
	}
	anomalies := call(t, reg, "timeseries.detect_anomalies", map[string]any{
		"path": "tiny.csv", "limit": float64(-1), "window": float64(-2), "threshold": float64(-3),
	})
	if anomalies.Status != "ok" {
		t.Fatalf("anomalies with negative args: %+v", anomalies)
	}
	analyze := call(t, reg, "timeseries.analyze", map[string]any{"path": "tiny.csv", "max_points": float64(-1)})
	if analyze.Status != "ok" {
		t.Fatalf("analyze with max_points=-1: %+v", analyze)
	}
}

func TestNegativeCorrelationKeepsSign(t *testing.T) {
	reg, root := testRegistry(t)
	var b strings.Builder
	b.WriteString("x,y\n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&b, "%d,%d\n", i, 7-i)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "corr.csv"), b.String())

	env := call(t, reg, "data.analyze", map[string]any{"path": "corr.csv"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Correlations []struct {
			R float64 `json:"r"`
		} `json:"correlations"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Correlations) == 0 {
		t.Fatal("expected a correlation pair")
	}
	if data.Correlations[0].R > -0.9 {
		t.Fatalf("expected a strongly negative correlation, got %v", data.Correlations[0].R)
	}
}

func TestRound2Negative(t *testing.T) {
	cases := map[float64]float64{
		-5:     -5,
		-1.234: -1.23,
		-2.678: -2.68,
		2.5:    2.5,
	}
	for input, want := range cases {
		if got := round2(input); got != want {
			t.Fatalf("round2(%v) = %v, want %v", input, got, want)
		}
	}
}

func TestKnowledgeCorpusScope(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "old.txt"), "Срок оказания услуги 30 дней.")
	mustWrite(t, filepath.Join(root, "data", "input", "new.txt"), "Срок оказания услуги 5 дней.")
	if env := call(t, reg, "knowledge.index", map[string]any{"path": "old.txt", "corpus": "old-task"}); env.Status != "ok" {
		t.Fatalf("index old: %+v", env)
	}
	if env := call(t, reg, "knowledge.index", map[string]any{"path": "new.txt", "corpus": "current-task"}); env.Status != "ok" {
		t.Fatalf("index new: %+v", env)
	}

	scoped := call(t, reg, "knowledge.search", map[string]any{"query": "срок оказания услуги", "corpus": "current-task"})
	if scoped.Status != "ok" {
		t.Fatalf("scoped search status %s data %+v", scoped.Status, scoped.Data)
	}
	raw, _ := json.Marshal(scoped.Data)
	var data struct {
		Hits []struct {
			Source string `json:"source"`
			Text   string `json:"text"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Hits) == 0 {
		t.Fatal("expected scoped hits")
	}
	for _, hit := range data.Hits {
		if hit.Source != "new.txt" {
			t.Fatalf("scoped search leaked a foreign document: %+v", hit.Source)
		}
		if !strings.Contains(hit.Text, "5 дней") {
			t.Fatalf("expected chunk text to carry the scoped statement: %q", hit.Text)
		}
	}

	unscoped := call(t, reg, "knowledge.search", map[string]any{"query": "срок оказания услуги"})
	rawAll, _ := json.Marshal(unscoped.Data)
	var all struct {
		Matched int `json:"matched"`
	}
	_ = json.Unmarshal(rawAll, &all)
	if all.Matched < 2 {
		t.Fatalf("unscoped search should see both corpora, got %d", all.Matched)
	}
}

func TestXLSXExcelDates(t *testing.T) {
	path := buildXLSXWithDates(t)
	sheets, warnings, err := readXLSXSheets(path)
	if err != nil {
		t.Fatalf("readXLSXSheets: %v (%v)", err, warnings)
	}
	if len(sheets) != 1 {
		t.Fatalf("expected 1 sheet, got %d", len(sheets))
	}
	tbl := sheets[0].Table
	if len(tbl.Rows) == 0 || len(tbl.Rows[0]) == 0 {
		t.Fatalf("no rows parsed: %+v", tbl)
	}
	if got := tbl.Rows[0][0]; got != "2026-01-01" {
		t.Fatalf("expected 2026-01-01 from Excel serial 46023, got %q", got)
	}
	if _, ok := parseTimeValue(tbl.Rows[0][0]); !ok {
		t.Fatalf("converted date must be parseable by the time-series loader: %q", tbl.Rows[0][0])
	}
}

func TestXLSXFormatClassification(t *testing.T) {
	none := fmtKind{}
	cases := []struct {
		code string
		want fmtKind
	}{
		{"#,##0.00;[Red]-#,##0.00", none},
		{"[Yellow]0.0", none},
		{`\d0`, none},
		{"yyyy-mm-dd", fmtKind{date: true}},
		{"m/d/yy h:mm", fmtKind{date: true}},
		{"hh:mm", fmtKind{time: true}},
		{"mm:ss", fmtKind{time: true}},
		{"mm", fmtKind{date: true}},
		{"mmm", fmtKind{date: true}},
		{"h:mm AM/PM", fmtKind{time: true}},
		{"[h]:mm:ss", fmtKind{elapsed: true}},
		{"[$-409]dd.mm.yyyy", fmtKind{date: true}},
	}
	for _, c := range cases {
		if got := classifyFormatCode(c.code); got != c.want {
			t.Fatalf("classifyFormatCode(%q) = %+v, want %+v", c.code, got, c.want)
		}
	}

	if got := builtinFormatKind(18); got != (fmtKind{time: true}) {
		t.Fatalf("builtin 18 (h:mm) must be time-only, got %+v", got)
	}
	if got := builtinFormatKind(45); got != (fmtKind{time: true}) {
		t.Fatalf("builtin 45 (mm:ss) is a clock time, got %+v", got)
	}
	if got := builtinFormatKind(46); got != (fmtKind{elapsed: true}) {
		t.Fatalf("builtin 46 ([h]:mm:ss) must be elapsed, got %+v", got)
	}
	if got := builtinFormatKind(47); got != (fmtKind{time: true}) {
		t.Fatalf("builtin 47 (mmss.0) is a clock time, got %+v", got)
	}
	if got := builtinFormatKind(14); got != (fmtKind{date: true}) {
		t.Fatalf("builtin 14 must be a date, got %+v", got)
	}
}

func TestExcelTimeAndElapsedFormatting(t *testing.T) {
	if got := formatExcelTime(0.5); got != "12:00:00" {
		t.Fatalf("formatExcelTime(0.5) = %q, want 12:00:00", got)
	}
	if got := formatExcelElapsed(1.5); got != "36:00:00" {
		t.Fatalf("formatExcelElapsed(1.5) = %q, want 36:00:00", got)
	}
	if got := formatExcelElapsed(0.25); got != "6:00:00" {
		t.Fatalf("formatExcelElapsed(0.25) = %q, want 6:00:00", got)
	}
}

func TestSlugifyTruncatesRunes(t *testing.T) {
	got := slugify(strings.Repeat("я", 70))
	if utf8.RuneCountInString(got) != 60 {
		t.Fatalf("expected 60 runes, got %d (%q)", utf8.RuneCountInString(got), got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("slug is not valid UTF-8: %q", got)
	}
}

func TestStddevLargeMagnitude(t *testing.T) {
	// Regression: the hand-rolled sqrt halved from x=v and was wrong for large
	// values (1e14 returned ~9.1e15 instead of 1e14).
	values := []float64{0, 1e14, 2e14}
	got := stddev(values)
	if got < 9.9e13 || got > 1.01e14 {
		t.Fatalf("stddev = %v, want ~1e14", got)
	}
	if r := pearson(values, values); r != 1 {
		t.Fatalf("pearson of identical columns = %v, want 1", r)
	}
	small := stddev([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	if small < 2.13 || small > 2.14 {
		t.Fatalf("stddev = %v, want ~2.138", small)
	}
}
