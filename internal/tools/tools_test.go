package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/observability"
	"hackathonkit.local/agentkit/internal/routing/fly"
	"hackathonkit.local/agentkit/internal/storage"
	"hackathonkit.local/agentkit/internal/telegram"
)

func testRegistry(t *testing.T) (*mcp.Registry, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configs", "routing.yaml"), []byte("routing:\n  mode: normal\n  version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"data/input", "data/artifacts", "data/index"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mgr := config.NewManager(root)
	reg := mcp.NewRegistry()
	RegisterAll(reg, Deps{
		Cfg:      mgr,
		Store:    storage.New(root, mgr.Config().Storage),
		Metrics:  observability.NewMetrics(),
		Router:   fly.NewRouter(mgr.Config().Routing),
		Learning: learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root)),
		Telegram: telegram.New(telegram.Settings{}),
	})
	return reg, root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func call(t *testing.T, reg *mcp.Registry, name string, args map[string]any) mcp.Envelope {
	t.Helper()
	tool, ok := reg.Get(name)
	if !ok {
		t.Fatalf("tool %s not registered", name)
	}
	ctx := observability.WithTrace(context.Background(), "test-trace")
	res, err := tool.Handler(ctx, args)
	if err != nil {
		t.Fatalf("tool %s returned go error: %v", name, err)
	}
	env, ok := res.(mcp.Envelope)
	if !ok {
		t.Fatalf("tool %s returned unexpected type %T", name, res)
	}
	return env
}

func TestSystemContext(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "system.context", map[string]any{})
	if env.Status != "ok" {
		t.Fatalf("status %s", env.Status)
	}
	data := env.Data.(map[string]any)
	if data["routing_mode"] != "normal" {
		t.Fatalf("expected normal mode, got %+v", data)
	}
	if len(data["capabilities"].([]string)) == 0 {
		t.Fatal("expected capabilities")
	}
}

func TestFlyRecommendTool(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "routing.fly_recommend", map[string]any{
		"task": "Find anomalies in equipment telemetry csv",
		"input_metadata": map[string]any{
			"types":         []any{"csv"},
			"has_timestamp": true,
		},
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["route"] != "timeseries" {
		t.Fatalf("expected timeseries route, got %+v", data)
	}
	if env.Metrics["max_score"].(float64) <= 0 {
		t.Fatalf("expected positive max_score, got %+v", env.Metrics)
	}
}

func TestTimeSeriesAnomalyDetection(t *testing.T) {
	reg, root := testRegistry(t)
	var b strings.Builder
	b.WriteString("ts,value\n")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 24; i++ {
		v := 10.0
		if i == 12 {
			v = 50
		}
		fmt.Fprintf(&b, "%s,%.1f\n", base.Add(time.Duration(i)*time.Hour).Format("2006-01-02 15:04:05"), v)
	}
	mustWrite(t, filepath.Join(root, "data", "input", "telemetry.csv"), b.String())
	env := call(t, reg, "timeseries.detect_anomalies", map[string]any{
		"path":      "telemetry.csv",
		"window":    float64(5),
		"threshold": 2.5,
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["anomaly_count"].(int) == 0 {
		t.Fatalf("expected anomalies, got %+v", data)
	}
	if len(env.Sources) == 0 {
		t.Fatal("expected anomaly sources")
	}
}

func TestDataInspectAndQuery(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "table.csv"),
		"name,value,date\nalfa,10,2026-01-01\nbeta,,2026-01-02\ngamma,30,2026-01-03\n")
	env := call(t, reg, "data.inspect", map[string]any{"path": "table.csv"})
	if env.Status != "ok" {
		t.Fatalf("status %s", env.Status)
	}
	data := env.Data.(map[string]any)
	if data["rows"].(int) != 3 || data["columns"].(int) != 3 {
		t.Fatalf("unexpected shape %+v", data)
	}
	profiles := data["profile"].([]columnProfile)
	if profiles[1].Kind != "numeric" || profiles[1].Missing != 1 {
		t.Fatalf("unexpected value profile %+v", profiles[1])
	}
	if profiles[2].Kind != "timestamp" {
		t.Fatalf("expected timestamp kind, got %+v", profiles[2])
	}
	q := call(t, reg, "data.query", map[string]any{
		"path":  "table.csv",
		"where": "value > 20",
	})
	qd := q.Data.(map[string]any)
	if qd["matched_rows"].(int) != 1 {
		t.Fatalf("expected one matched row, got %+v", qd)
	}
}

func TestKnowledgeIndexAndSearch(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "contract.txt"),
		"Договор поставки оборудования. Поставщик обязуется передать оборудование в срок. "+
			"Оборудование должно соответствовать техническому заданию и стандартам качества. "+
			"Приемка оборудования проводится комиссией.")
	idx := call(t, reg, "knowledge.index", map[string]any{"path": "contract.txt"})
	if idx.Status != "ok" {
		t.Fatalf("index status %s data %+v", idx.Status, idx.Data)
	}
	search := call(t, reg, "knowledge.search", map[string]any{"query": "оборудование качество"})
	if search.Status != "ok" {
		t.Fatalf("search status %s data %+v warnings %v", search.Status, search.Data, search.Warnings)
	}
	data := search.Data.(map[string]any)
	if data["matched"].(int) == 0 {
		t.Fatalf("expected hits, got %+v", data)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "documents.extract", map[string]any{"path": "../../secret.txt"})
	if env.Status != "error" {
		t.Fatalf("expected error for path traversal, got %+v", env)
	}
}

func TestChartsAndReports(t *testing.T) {
	reg, root := testRegistry(t)
	chart := call(t, reg, "charts.create", map[string]any{
		"title": "Telemetry",
		"series": []any{
			map[string]any{
				"label": "sensor",
				"points": []any{
					map[string]any{"t": "2026-01-01T00:00:00Z", "v": 10.0},
					map[string]any{"t": "2026-01-01T01:00:00Z", "v": 12.5},
				},
			},
		},
	})
	if chart.Status != "ok" {
		t.Fatalf("chart status %s data %+v", chart.Status, chart.Data)
	}
	chartData := chart.Data.(map[string]any)
	svgPath := filepath.Join(root, "data", "artifacts", filepath.FromSlash(chartData["artifact_id"].(string)))
	if _, err := os.Stat(svgPath); err != nil {
		t.Fatalf("svg artifact missing: %v", err)
	}
	report := call(t, reg, "reports.create", map[string]any{
		"title": "Anomaly report",
		"sections": []any{
			map[string]any{"heading": "Findings", "body": "Two anomalies detected."},
		},
	})
	if report.Status != "ok" {
		t.Fatalf("report status %s data %+v", report.Status, report.Data)
	}
}
