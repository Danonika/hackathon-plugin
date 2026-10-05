package tools

import (
	"io"
	"strings"
	"testing"
)

func TestCommaIsThousandsSigned(t *testing.T) {
	for _, s := range []string{"-123,456", "+123,456", "123,456"} {
		if !commaIsThousands(s) {
			t.Fatalf("commaIsThousands(%q) = false, want true", s)
		}
	}
	if commaIsThousands("1,5") {
		t.Fatal("1,5 must be a decimal mark, not thousands")
	}
}

func TestArgIntRejectsFractions(t *testing.T) {
	if got := argInt(map[string]any{"v": 3.5}, "v", 7); got != 7 {
		t.Fatalf("argInt fractional = %d, want default 7", got)
	}
	if got := argInt(map[string]any{"v": 3.0}, "v", 7); got != 3 {
		t.Fatalf("argInt integral = %d, want 3", got)
	}
}

func TestSnippetNonPositiveLimit(t *testing.T) {
	if got := snippet("abcdef", 0); got != "" {
		t.Fatalf("snippet limit 0 = %q, want empty", got)
	}
	if got := snippet("abcdef", -1); got != "" {
		t.Fatalf("snippet negative limit = %q, want empty", got)
	}
}

func TestMinMaxEmpty(t *testing.T) {
	if minOf(nil) != 0 || maxOf(nil) != 0 {
		t.Fatal("minOf/maxOf of empty slice must be 0")
	}
}

func TestPathSegmentUniquenessAfterTruncation(t *testing.T) {
	prefix := strings.Repeat("a", 80)
	a := pathSegment(prefix + "1")
	b := pathSegment(prefix + "2")
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("pathSegment lengths = %d, %d, want 64", len(a), len(b))
	}
	if a == b {
		t.Fatalf("distinct long values collapsed to %q", a)
	}
}

func TestChunkSnippetCentersOnMatch(t *testing.T) {
	text := strings.Repeat("x", 100) + "needle" + strings.Repeat("y", 100)
	got := chunkSnippet(text, map[string]bool{"needle": true}, 40)
	if !strings.Contains(got, "needle") {
		t.Fatalf("chunkSnippet does not contain the match: %q", got)
	}
	if len([]rune(got)) > 43 {
		t.Fatalf("chunkSnippet exceeded limit: %d", len([]rune(got)))
	}
}

func TestMaxBytesReaderExactLimitEOF(t *testing.T) {
	exact := &maxBytesReader{r: strings.NewReader("abcd"), remaining: 4}
	got, err := io.ReadAll(exact)
	if err != nil {
		t.Fatalf("exact limit read: %v", err)
	}
	if string(got) != "abcd" {
		t.Fatalf("exact limit read = %q", got)
	}

	over := &maxBytesReader{r: strings.NewReader("abcde"), remaining: 4}
	if _, err := io.ReadAll(over); err == nil {
		t.Fatal("over-limit read must fail")
	}
}
