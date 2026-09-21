package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeSeries(n int, values []float64) []seriesPoint {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pts := make([]seriesPoint, 0, n)
	for i := 0; i < n; i++ {
		v := 0.0
		if i < len(values) {
			v = values[i]
		}
		pts = append(pts, seriesPoint{T: base.Add(time.Duration(i) * time.Hour), V: v})
	}
	return pts
}

func TestDownsampleAndSlopeEdgeCases(t *testing.T) {
	pts := makeSeries(10, nil)
	if got := downsample(pts, 3); len(got) != 3 {
		t.Fatalf("downsample(10,3) = %d points", len(got))
	}
	if got := downsample(pts, 0); len(got) != 10 {
		t.Fatalf("downsample(10,0) = %d points", len(got))
	}
	if got := downsample(pts, 20); len(got) != 10 {
		t.Fatalf("downsample(10,20) = %d points", len(got))
	}
	first := downsample(pts, 3)[0]
	if !first.T.Equal(pts[0].T) {
		t.Fatalf("downsample must keep the first point, got %v", first.T)
	}
	if slope := seriesSlopePerHour(makeSeries(1, []float64{5})); slope != 0 {
		t.Fatalf("slope of a single point = %v", slope)
	}
	sameTime := []seriesPoint{{T: pts[0].T, V: 1}, {T: pts[0].T, V: 2}}
	if slope := seriesSlopePerHour(sameTime); slope != 0 {
		t.Fatalf("slope with identical timestamps = %v", slope)
	}
	if slope := seriesSlopePerHour(makeSeries(5, []float64{0, 1, 2, 3, 4})); slope <= 0 {
		t.Fatalf("expected positive slope, got %v", slope)
	}
}

func TestQuantile(t *testing.T) {
	if got := quantile(nil, 0.5); got != 0 {
		t.Fatalf("quantile(empty) = %v", got)
	}
	if got := quantile([]float64{5}, 0.9); got != 5 {
		t.Fatalf("quantile(single) = %v", got)
	}
	if got := quantile([]float64{1, 2, 3, 4}, 0.5); got != 2.5 {
		t.Fatalf("quantile median = %v", got)
	}
	if got := quantile([]float64{1, 2, 3, 4}, 1.0); got != 4 {
		t.Fatalf("quantile(1.0) = %v", got)
	}
}

func writeSeriesCSV(t *testing.T, root, name string, rows [][2]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("ts_value,reading\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "%s,%s\n", row[0], row[1])
	}
	mustWrite(t, filepath.Join(root, "data", "input", name), b.String())
}

func TestTimeSeriesIQROutliers(t *testing.T) {
	reg, root := testRegistry(t)
	rows := make([][2]string, 0, 20)
	for i := 1; i <= 20; i++ {
		v := i
		if i == 11 {
			v = 1000
		}
		rows = append(rows, [2]string{
			time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", v),
		})
	}
	writeSeriesCSV(t, root, "iqr.csv", rows)

	env := call(t, reg, "timeseries.detect_anomalies", map[string]any{
		"path":   "iqr.csv",
		"method": "IQR",
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["method"] != "iqr" {
		t.Fatalf("method = %v", data["method"])
	}
	if data["anomaly_count"].(int) != 1 {
		t.Fatalf("expected exactly one IQR outlier, got %+v", data)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Anomalies []struct {
			Value  float64 `json:"value"`
			Score  float64 `json:"score"`
			Method string  `json:"method"`
		} `json:"anomalies"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Anomalies) != 1 {
		t.Fatalf("decoded anomalies = %+v", decoded.Anomalies)
	}
	if a := decoded.Anomalies[0]; a.Method != "iqr" || a.Value != 1000 || a.Score <= 0 {
		t.Fatalf("unexpected anomaly %+v", a)
	}
	if len(env.Sources) == 0 {
		t.Fatal("expected anomaly sources")
	}
}

func TestTimeSeriesZScoreZeroStddev(t *testing.T) {
	reg, root := testRegistry(t)
	constant := make([][2]string, 0, 6)
	for i := 0; i < 6; i++ {
		v := 5.0
		if i == 5 {
			v = 9.0
		}
		constant = append(constant, [2]string{
			time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%.1f", v),
		})
	}
	writeSeriesCSV(t, root, "constant.csv", constant)

	env := call(t, reg, "timeseries.detect_anomalies", map[string]any{"path": "constant.csv"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["anomaly_count"].(int) != 1 {
		t.Fatalf("expected the changed value to be an anomaly, got %+v", data)
	}
}

func TestTimeSeriesAnalyzeExplicitColumnsAndCap(t *testing.T) {
	reg, root := testRegistry(t)
	rows := make([][2]string, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, [2]string{
			time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", i*10),
		})
	}
	writeSeriesCSV(t, root, "explicit.csv", rows)

	env := call(t, reg, "timeseries.analyze", map[string]any{
		"path":         "explicit.csv",
		"time_column":  "ts_value",
		"value_column": "reading",
		"max_points":   float64(4),
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["time_column"] != "ts_value" || data["value_column"] != "reading" {
		t.Fatalf("unexpected columns %+v", data)
	}
	if data["count"].(int) != 10 {
		t.Fatalf("count = %v", data["count"])
	}
	if got := len(data["series"].([]map[string]any)); got != 4 {
		t.Fatalf("series = %d points, want 4", got)
	}
	if data["min"].(float64) != 0 || data["max"].(float64) != 90 {
		t.Fatalf("range = %v..%v", data["min"], data["max"])
	}
	if data["slope_per_hour"].(float64) <= 0 {
		t.Fatalf("expected positive slope, got %v", data["slope_per_hour"])
	}
	if len(env.Sources) == 0 || env.Sources[0].Column != "reading" {
		t.Fatalf("expected a column source, got %+v", env.Sources)
	}

	capped := call(t, reg, "timeseries.analyze", map[string]any{
		"path":       "explicit.csv",
		"max_points": float64(999999),
	})
	if got := len(capped.Data.(map[string]any)["series"].([]map[string]any)); got != 10 {
		t.Fatalf("max_points above cap should still return all 10 points, got %d", got)
	}
}

func TestTimeSeriesLoadErrors(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "textonly.csv"), "name,city\nalfa,astana\nbeta,almaty\ngamma,taraz\n")
	if env := call(t, reg, "timeseries.analyze", map[string]any{"path": "textonly.csv"}); env.Status != "error" {
		t.Fatalf("text-only table must fail, got %+v", env)
	}

	writeSeriesCSV(t, root, "two.csv", [][2]string{
		{"2026-01-01 00:00:00", "1"},
		{"2026-01-01 01:00:00", "2"},
	})
	if env := call(t, reg, "timeseries.analyze", map[string]any{"path": "two.csv"}); env.Status != "error" {
		t.Fatalf("two rows must fail, got %+v", env)
	}
}

func TestLoadSeriesSkipsInvalidRows(t *testing.T) {
	tbl := &table{
		Headers: []string{"when", "reading"},
		Rows: [][]string{
			{"2026-01-01 00:00:00", "1"},
			{"bad-date", "2"},
			{"2026-01-01 02:00:00", ""},
			{"2026-01-01 03:00:00", "3"},
			{"2026-01-01 04:00:00", "4"},
		},
	}
	points, timeCol, valueCol, err := loadSeries(tbl, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 {
		t.Fatalf("expected 3 valid points, got %d", len(points))
	}
	if timeCol != "when" || valueCol != "reading" {
		t.Fatalf("auto-detected columns = %q, %q", timeCol, valueCol)
	}
}
