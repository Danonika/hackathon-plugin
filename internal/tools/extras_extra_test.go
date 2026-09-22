package tools

import "testing"

func TestParseChartSeriesDefaults(t *testing.T) {
	parsed, _, err := parseChartSeries(map[string]any{"series": []any{
		map[string]any{"label": "   ", "points": []any{map[string]any{"y": 1.0}}},
		map[string]any{"label": "named", "points": []any{map[string]any{"y": 2.0}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 || parsed[0].Label != "series" || parsed[1].Label != "named" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestRenderChartPicksLongestSeries(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "charts.create", map[string]any{
		"title": "longest",
		"series": []any{
			map[string]any{"label": "short", "points": []any{map[string]any{"y": 1.0}}},
			map[string]any{"label": "long", "points": []any{
				map[string]any{"y": 1.0},
				map[string]any{"y": 2.0},
				map[string]any{"y": 3.0},
				map[string]any{"y": 4.0},
			}},
		},
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	if env.Data.(map[string]any)["points"].(int) != 5 {
		t.Fatalf("points = %+v", env.Data)
	}
}
