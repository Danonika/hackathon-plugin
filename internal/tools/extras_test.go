package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassificationTool(t *testing.T) {
	reg, _ := testRegistry(t)

	multi := call(t, reg, "classification.classify", map[string]any{
		"text": "Подозрительная транзакция: мошеннич схема списания",
	})
	if multi.Status != "ok" {
		t.Fatalf("status %s data %+v", multi.Status, multi.Data)
	}
	data := multi.Data.(map[string]any)
	if data["label"] != "finance" {
		t.Fatalf("label = %v, want finance (most hits)", data["label"])
	}
	scores := data["scores"].(map[string]float64)
	if scores["finance"] <= 0 || scores["security"] <= 0 {
		t.Fatalf("expected both scores, got %+v", scores)
	}
	if matched := data["matched_keywords"].([]string); len(matched) == 0 {
		t.Fatalf("expected matched keywords, got %+v", data)
	}

	builtin := call(t, reg, "classification.classify", map[string]any{
		"text": "cve exploit found", "categories": []any{"security"},
	})
	if builtin.Data.(map[string]any)["label"] != "security" {
		t.Fatalf("builtin category label = %+v", builtin.Data)
	}

	custom := call(t, reg, "classification.classify", map[string]any{
		"text": "Health Risk assessment", "categories": []any{"Health Risk"},
	})
	if got := custom.Data.(map[string]any)["label"]; got != "health risk" {
		t.Fatalf("custom category label = %v, want health risk", got)
	}

	unknown := call(t, reg, "classification.classify", map[string]any{"text": "zzz qqq"})
	if unknown.Status != "ok" {
		t.Fatalf("no-match status = %s, want ok with warning (%+v)", unknown.Status, unknown)
	}
	if unknown.Data.(map[string]any)["label"] != "unknown" {
		t.Fatalf("no-match label = %+v", unknown.Data)
	}
	if len(unknown.Warnings) == 0 {
		t.Fatalf("expected warning for no match, got %+v", unknown)
	}

	if env := call(t, reg, "classification.classify", map[string]any{"text": ""}); env.Status != "error" {
		t.Fatalf("empty text must fail, got %+v", env)
	}
}

func TestChartsToolVariants(t *testing.T) {
	reg, root := testRegistry(t)

	bar := call(t, reg, "charts.create", map[string]any{
		"title":      "Load <per> & hour",
		"chart_type": "bar",
		"series": []any{
			map[string]any{
				"label": "sensor",
				"points": []any{
					map[string]any{"x": 1.5, "y": 10},
					map[string]any{"t": "2026-01-01T01:00:00Z", "v": 20.0},
					map[string]any{"t": "a-very-long-category-label-value", "v": -5.0},
				},
			},
		},
	})
	if bar.Status != "ok" {
		t.Fatalf("bar status %s data %+v warnings %v", bar.Status, bar.Data, bar.Warnings)
	}
	barData := bar.Data.(map[string]any)
	if barData["chart_type"] != "bar" || barData["series"].(int) != 1 || barData["points"].(int) != 3 {
		t.Fatalf("bar data = %+v", barData)
	}
	barPath := filepath.Join(root, "data", "artifacts", filepath.FromSlash(barData["artifact_id"].(string)))
	raw, err := os.ReadFile(barPath)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(raw)
	if !strings.Contains(svg, "<rect") || !strings.Contains(svg, "&lt;per&gt;") {
		t.Fatalf("bar svg missing expected content: %.200s", svg)
	}
	if len(bar.Sources) == 0 {
		t.Fatal("chart must carry a source")
	}

	line := call(t, reg, "charts.create", map[string]any{
		"title": "two lines",
		"series": []any{
			map[string]any{
				"points": []any{
					map[string]any{"x": "a", "y": 1.0},
					map[string]any{"x": "b", "y": 2.0},
				},
			},
			map[string]any{
				"label": "second",
				"points": []any{
					map[string]any{"x": "a", "y": 3.0},
					map[string]any{"x": "b", "y": 1.0},
				},
			},
		},
	})
	if line.Status != "ok" {
		t.Fatalf("line status %s data %+v", line.Status, line.Data)
	}
	if line.Data.(map[string]any)["series"].(int) != 2 {
		t.Fatalf("line series = %+v", line.Data)
	}

	single := call(t, reg, "charts.create", map[string]any{
		"series": []any{
			map[string]any{"points": []any{map[string]any{"y": 42.0}}},
		},
	})
	if single.Status != "ok" {
		t.Fatalf("single point status %s data %+v", single.Status, single.Data)
	}
	if single.Data.(map[string]any)["chart_type"] != "line" {
		t.Fatalf("default chart type = %+v", single.Data)
	}
}

func TestChartsToolErrors(t *testing.T) {
	reg, _ := testRegistry(t)
	for name, args := range map[string]map[string]any{
		"missing series":  {},
		"empty series":    {"series": []any{}},
		"no points":       {"series": []any{map[string]any{"label": "a", "points": []any{}}}},
		"non-map series":  {"series": []any{"nope"}},
		"point not a map": {"series": []any{map[string]any{"points": []any{"bad"}}}},
	} {
		env := call(t, reg, "charts.create", args)
		if env.Status != "error" {
			t.Fatalf("%s: status %s, want error (%+v)", name, env.Status, env)
		}
	}
}

func TestParseChartPointsDefaults(t *testing.T) {
	points := parseChartPoints([]any{
		map[string]any{"x": "a", "y": 1.0},
		map[string]any{"x": 3.0, "y": 2},
		map[string]any{"t": "2026-01-01", "v": 4.0},
		map[string]any{"y": "bad"},
		"not-a-map",
	})
	if len(points) != 4 {
		t.Fatalf("points = %+v", points)
	}
	if points[0].X != "a" || points[1].X != "3" || points[2].X != "2026-01-01" {
		t.Fatalf("x parsing = %+v", points)
	}
	if points[0].Y != 1 || points[1].Y != 2 || points[2].Y != 4 {
		t.Fatalf("y parsing = %+v", points)
	}
	if points[3].X != "" || points[3].Y != 0 {
		t.Fatalf("default point = %+v", points[3])
	}
	if got := parseChartPoints("nope"); got != nil {
		t.Fatalf("non-array points = %+v", got)
	}
}

func TestReportsToolFormats(t *testing.T) {
	reg, root := testRegistry(t)

	md := call(t, reg, "reports.create", map[string]any{
		"title": "Anomaly Digest",
		"sections": []any{
			map[string]any{"heading": "Findings", "body": "Two anomalies detected."},
			map[string]any{"body": "No heading section."},
		},
	})
	if md.Status != "ok" {
		t.Fatalf("md status %s data %+v warnings %v", md.Status, md.Data, md.Warnings)
	}
	mdData := md.Data.(map[string]any)
	if mdData["sections"].(int) != 2 {
		t.Fatalf("sections = %+v", mdData)
	}
	mdRaw, err := os.ReadFile(filepath.Join(root, "data", "artifacts", filepath.FromSlash(mdData["artifact_id"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	mdText := string(mdRaw)
	if !strings.Contains(mdText, "# Anomaly Digest") || !strings.Contains(mdText, "## Findings") {
		t.Fatalf("unexpected markdown report: %q", mdText)
	}

	html := call(t, reg, "reports.create", map[string]any{
		"title":  "HTML <Report>",
		"format": "HTML",
		"sections": []any{
			map[string]any{"heading": "Body", "body": "first para\n\nsecond para\n\n"},
			map[string]any{"heading": "", "body": ""},
			"not-a-map",
		},
	})
	if html.Status != "ok" {
		t.Fatalf("html status %s data %+v", html.Status, html.Data)
	}
	htmlData := html.Data.(map[string]any)
	if htmlData["sections"].(int) != 1 {
		t.Fatalf("html sections = %+v", htmlData)
	}
	htmlRaw, err := os.ReadFile(filepath.Join(root, "data", "artifacts", filepath.FromSlash(htmlData["artifact_id"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	htmlText := string(htmlRaw)
	if !strings.Contains(htmlText, "<title>HTML &lt;Report&gt;</title>") ||
		!strings.Contains(htmlText, "<h1>HTML &lt;Report&gt;</h1>") ||
		!strings.Contains(htmlText, "<p>first para</p>") ||
		!strings.Contains(htmlText, "<p>second para</p>") {
		t.Fatalf("unexpected html report: %q", htmlText)
	}
	if strings.Contains(htmlText, "<h2></h2>") {
		t.Fatalf("blank heading must be skipped: %q", htmlText)
	}
	if len(html.Sources) == 0 {
		t.Fatal("report must carry a source")
	}
}

func TestReportsToolErrors(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "reports.create", map[string]any{"title": "", "sections": []any{map[string]any{"heading": "a"}}})
	if env.Status != "error" {
		t.Fatalf("missing title must fail: %+v", env)
	}
	env = call(t, reg, "reports.create", map[string]any{"title": "t"})
	if env.Status != "error" {
		t.Fatalf("missing sections must fail: %+v", env)
	}
	env = call(t, reg, "reports.create", map[string]any{"title": "t", "sections": []any{map[string]any{"heading": " ", "body": ""}, "x"}})
	if env.Status != "error" {
		t.Fatalf("all-invalid sections must fail: %+v", env)
	}
}

func TestMediaToolsUnconfigured(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "clip.mp3"), "not-really-audio")
	mustWrite(t, filepath.Join(root, "data", "input", "photo.png"), "not-really-an-image")

	audio := call(t, reg, "audio.transcribe", map[string]any{"path": "clip.mp3"})
	if audio.Status != "error" {
		t.Fatalf("unconfigured audio status %s, want error (%+v)", audio.Status, audio)
	}
	if !strings.Contains(audio.Data.(map[string]any)["error"].(string), "OPENAI_API_KEY is not configured") {
		t.Fatalf("unexpected audio error %+v", audio.Data)
	}

	vision := call(t, reg, "vision.analyze", map[string]any{"path": "photo.png", "prompt": "describe"})
	if vision.Status != "error" {
		t.Fatalf("unconfigured vision status %s, want error (%+v)", vision.Status, vision)
	}
	if !strings.Contains(vision.Data.(map[string]any)["error"].(string), "OPENAI_API_KEY is not configured") {
		t.Fatalf("unexpected vision error %+v", vision.Data)
	}

	for name, args := range map[string]map[string]any{
		"audio missing path":  {"path": ""},
		"vision missing path": {},
	} {
		env := call(t, reg, map[string]string{"audio missing path": "audio.transcribe", "vision missing path": "vision.analyze"}[name], args)
		if env.Status != "error" {
			t.Fatalf("%s must fail: %+v", name, env)
		}
	}
	if env := call(t, reg, "audio.transcribe", map[string]any{"path": "../../secret.mp3"}); env.Status != "error" {
		t.Fatalf("audio traversal must fail: %+v", env)
	}
	if env := call(t, reg, "vision.analyze", map[string]any{"path": "../../secret.png"}); env.Status != "error" {
		t.Fatalf("vision traversal must fail: %+v", env)
	}
}

func TestMimeForExt(t *testing.T) {
	cases := map[string]string{
		".png":  "image/png",
		".PNG":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
		".bmp":  "image/bmp",
		".tiff": "application/octet-stream",
		"":      "application/octet-stream",
	}
	for ext, want := range cases {
		if got := mimeForExt(ext); got != want {
			t.Fatalf("mimeForExt(%q) = %q, want %q", ext, got, want)
		}
	}
}
