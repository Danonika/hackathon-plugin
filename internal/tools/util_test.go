package tools

import (
	"testing"
	"time"
)

func TestParseNumber(t *testing.T) {
	cases := []struct {
		in    string
		want  float64
		valid bool
	}{
		{"42", 42, true},
		{" 3.5 ", 3.5, true},
		{"1,5", 1.5, true},
		{"-7", -7, true},
		{"1e3", 1000, true},
		{"", 0, false},
		{"-", 0, false},
		{"abc", 0, false},
		{"1 000", 0, false},
	}
	for _, c := range cases {
		got, ok := parseNumber(c.in)
		if ok != c.valid || (ok && got != c.want) {
			t.Fatalf("parseNumber(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.valid)
		}
	}
}

func TestParseTimeValueLayouts(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		valid bool
	}{
		{"2026-01-01T10:20:30Z", "2026-01-01T10:20:30Z", true},
		{"2026-01-01T10:20:30", "2026-01-01T10:20:30Z", true},
		{"2026-01-01 10:20:30", "2026-01-01T10:20:30Z", true},
		{"2026-01-01 10:20", "2026-01-01T10:20:00Z", true},
		{"2026-01-01", "2026-01-01T00:00:00Z", true},
		{"02.01.2026 10:20:30", "2026-01-02T10:20:30Z", true},
		{"02.01.2026 10:20", "2026-01-02T10:20:00Z", true},
		{"02.01.2026", "2026-01-02T00:00:00Z", true},
		{"01/02/2026 10:20:30", "2026-01-02T10:20:30Z", true},
		{"01/02/2026", "2026-01-02T00:00:00Z", true},
		{"Mon, 02 Jan 2026 15:04:05 UTC", "2026-01-02T15:04:05Z", true},
		{"02 Jan 26 15:04 UTC", "2026-01-02T15:04:00Z", true},
		{"", "", false},
		{"not-a-date", "", false},
		{"99", "", false},
	}
	for _, c := range cases {
		got, ok := parseTimeValue(c.in)
		if ok != c.valid {
			t.Fatalf("parseTimeValue(%q) valid = %v, want %v", c.in, ok, c.valid)
		}
		if ok && got.UTC().Format(time.RFC3339) != c.want {
			t.Fatalf("parseTimeValue(%q) = %s, want %s", c.in, got.UTC().Format(time.RFC3339), c.want)
		}
	}

	unix, ok := parseTimeValue("1700000000")
	if !ok || unix.Unix() != 1700000000 {
		t.Fatalf("expected unix seconds parse, got %v %v", unix, ok)
	}
	millis, ok := parseTimeValue("1700000000000")
	if !ok || millis.UnixMilli() != 1700000000000 {
		t.Fatalf("expected unix millis parse, got %v %v", millis, ok)
	}
}

func TestXmlEscapeAndHashAndSlug(t *testing.T) {
	if got := xmlEscape(`<a href="x">&'`); got != "&lt;a href=&quot;x&quot;&gt;&amp;&apos;" {
		t.Fatalf("xmlEscape = %q", got)
	}
	if shortHash("abc") != shortHash("abc") {
		t.Fatal("shortHash must be deterministic")
	}
	if shortHash("abc") == shortHash("abd") {
		t.Fatal("shortHash collision for distinct inputs")
	}
	if shortHash("") == "" {
		t.Fatal("shortHash must not be empty")
	}
	if got := slugify(""); got != "item" {
		t.Fatalf("slugify empty = %q", got)
	}
	if got := slugify("Hello,  World!!"); got != "hello-world" {
		t.Fatalf("slugify punctuation = %q", got)
	}
	if got := slugify("Привет Мир"); got != "привет-мир" {
		t.Fatalf("slugify cyrillic = %q", got)
	}
}

func TestSnippetAndTrimPartialRune(t *testing.T) {
	if got := snippet("  hello  ", 10); got != "hello" {
		t.Fatalf("snippet short = %q", got)
	}
	if got := snippet("abcdef", 3); got != "abc..." {
		t.Fatalf("snippet long = %q", got)
	}
	multibyte := "末尾"
	if got := trimPartialRune(multibyte[:len(multibyte)-1]); got != "末" {
		t.Fatalf("trimPartialRune = %q", got)
	}
	if got := trimPartialRune("plain"); got != "plain" {
		t.Fatalf("trimPartialRune plain = %q", got)
	}
	if got := trimPartialRune(""); got != "" {
		t.Fatalf("trimPartialRune empty = %q", got)
	}
}

func TestStatsEdgeCases(t *testing.T) {
	if mean(nil) != 0 {
		t.Fatal("mean(nil) must be 0")
	}
	if stddev([]float64{5}) != 0 {
		t.Fatal("stddev of one value must be 0")
	}
	if pearson([]float64{1, 2, 3}, []float64{1, 2}) != 0 {
		t.Fatal("pearson mismatched lengths must be 0")
	}
	if pearson([]float64{2, 2, 2}, []float64{1, 2, 3}) != 0 {
		t.Fatal("pearson constant input must be 0")
	}
	if sqrt(-4) != 0 {
		t.Fatal("sqrt of negative must be 0")
	}
	if got := sqrt(1e14); got != 1e7 {
		t.Fatalf("sqrt(1e14) = %v", got)
	}
}
