package tools

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
	"testing"
)

func TestNativeIntBounds(t *testing.T) {
	for _, v := range []any{math.MaxInt, strconv.Itoa(math.MaxInt)} {
		if n := argInt(map[string]any{"n": v}, "n", 0); n != math.MaxInt {
			t.Errorf("max int %v became %d", v, n)
		}
	}
	for _, v := range []any{math.Exp2(strconv.IntSize - 1), strconv.FormatUint(uint64(math.MaxInt)+1, 10)} {
		if n := argInt(map[string]any{"n": v}, "n", 0); n != 0 {
			t.Errorf("out of range %v became %d", v, n)
		}
	}
}

func TestRelativeRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveRepoPath(".", "sub/file")
	if err != nil || got != filepath.Join(canonicalRoot, "sub", "file") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestInvalidOptionalArgumentsAndDuplicateColumns(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "profile.csv"), "Value,Other\n1,0\n2,1\n")
	if env := call(t, reg, "data.inspect", map[string]any{"path": "profile.csv", "columns": []any{"Value", 1}}); env.Status != "error" {
		t.Fatalf("accepted invalid columns: %+v", env)
	}
	env := call(t, reg, "data.inspect", map[string]any{"path": "profile.csv", "columns": []any{"Value", "value", "Val"}})
	if env.Status != "ok" || len(env.Data.(map[string]any)["profile"].([]columnProfile)) != 1 {
		t.Fatalf("duplicate profile: %+v", env)
	}
	for _, args := range []map[string]any{
		{"slug": "bad", "stages": []any{map[string]any{"goal": "missing ID"}}},
		{"slug": "bad", "mandatory_scenario": "wrong type"},
		{"slug": "bad", "overwrite": "false"},
	} {
		if env := call(t, reg, "plan.create", args); env.Status != "error" {
			t.Fatalf("accepted malformed plan: %+v", env)
		}
	}
}

func TestChartCoordinatesAndDroppedPoints(t *testing.T) {
	series, warnings, err := parseChartSeries(map[string]any{"series": []any{map[string]any{"points": []any{
		map[string]any{"t": 3, "v": json.Number("4.5")},
		map[string]any{"x": json.Number("4"), "y": "unusable"},
	}}}})
	if err != nil || len(series) != 1 || len(warnings) != 1 || len(series[0].Points) != 1 || series[0].Points[0] != (chartPoint{X: "3", Y: 4.5}) {
		t.Fatalf("chart: %+v, %v, %v", series, warnings, err)
	}
}
