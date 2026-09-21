package tools

import "testing"

func TestArgumentHelpers(t *testing.T) {
	if got := argFloat(map[string]any{"v": int(3)}, "v", 1); got != 3 {
		t.Fatalf("argFloat int = %v", got)
	}
	if got := argFloat(map[string]any{"v": "2.5"}, "v", 1); got != 2.5 {
		t.Fatalf("argFloat string = %v", got)
	}
	if got := argFloat(map[string]any{"v": "bad"}, "v", 1); got != 1 {
		t.Fatalf("argFloat bad string = %v", got)
	}
	if got := argFloat(map[string]any{}, "v", 4); got != 4 {
		t.Fatalf("argFloat default = %v", got)
	}
	if got := argInt(map[string]any{"v": "7"}, "v", 1); got != 7 {
		t.Fatalf("argInt = %v", got)
	}
	if !argBool(map[string]any{"b": true}, "b", false) {
		t.Fatal("argBool true")
	}
	if !argBool(map[string]any{}, "b", true) {
		t.Fatal("argBool default")
	}
	if got := argMap(map[string]any{"m": map[string]any{"x": 1}}, "m")["x"]; got != 1 {
		t.Fatalf("argMap = %v", got)
	}
	if argMap(map[string]any{}, "m") != nil {
		t.Fatal("argMap missing must be nil")
	}

	if got := argStringSlice(map[string]any{"v": []string{"a", "b"}}, "v"); len(got) != 2 {
		t.Fatalf("argStringSlice []string = %v", got)
	}
	if got := argStringSlice(map[string]any{"v": []any{"a", 2, "c"}}, "v"); len(got) != 2 {
		t.Fatalf("argStringSlice mixed = %v", got)
	}
	if got := argStringSlice(map[string]any{"v": "solo"}, "v"); len(got) != 1 || got[0] != "solo" {
		t.Fatalf("argStringSlice string = %v", got)
	}
	if got := argStringSlice(map[string]any{"v": ""}, "v"); got != nil {
		t.Fatalf("argStringSlice empty string = %v", got)
	}
	if got := argStringSlice(map[string]any{"v": 5}, "v"); got != nil {
		t.Fatalf("argStringSlice unsupported = %v", got)
	}
}

func TestSmallMathAndTextHelpers(t *testing.T) {
	if estimateTokens(0) != 0 || estimateTokens(-1) != 0 || estimateTokens(5) != 2 {
		t.Fatalf("estimateTokens edge cases")
	}
	if absFloat(-3) != 3 || absFloat(3) != 3 || absFloat(0) != 0 {
		t.Fatal("absFloat edge cases")
	}
	if minOf([]float64{5, 1, 3, -2}) != -2 {
		t.Fatal("minOf must find the minimum")
	}
	if maxOf([]float64{5, 1, 9}) != 9 {
		t.Fatal("maxOf must find the maximum")
	}
	if got := frontmatterDescription("---\nname: x\ndescription: \"demo skill\"\n---\n"); got != "demo skill" {
		t.Fatalf("frontmatterDescription = %q", got)
	}
	if got := frontmatterDescription("no frontmatter here"); got != "" {
		t.Fatalf("frontmatterDescription missing = %q", got)
	}
	recordTelegramError(Deps{})
}
