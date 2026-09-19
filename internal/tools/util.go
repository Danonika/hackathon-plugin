package tools

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, false
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, true
	}
	if v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64); err == nil {
		return v, true
	}
	return 0, false
}

var timeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"02.01.2006 15:04:05",
	"02.01.2006 15:04",
	"02.01.2006",
	"01/02/2006 15:04:05",
	"01/02/2006",
	time.RFC1123,
	time.RFC822,
}

func parseTimeValue(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		switch {
		case v > 1e12:
			return time.UnixMilli(v), true
		case v > 1e9:
			return time.Unix(v, 0), true
		}
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func timestampHeader(name string) bool {
	n := strings.ToLower(name)
	for _, kw := range []string{"time", "date", "timestamp", "datetime", "ts_", "дата", "время", "период"} {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return strconv.FormatUint(uint64(h.Sum32()), 16)
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r >= 'а' && r <= 'я' || r == 'ё' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	runes := []rune(out)
	if len(runes) > 60 {
		out = string(runes[:60])
	}
	return out
}

func snippet(text string, limit int) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) <= limit {
		return string(r)
	}
	return string(r[:limit]) + "..."
}

// trimPartialRune drops a trailing byte fragment of a multi-byte UTF-8
// sequence left behind by byte-based truncation.
func trimPartialRune(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size != 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	sum := 0.0
	for _, v := range values {
		d := v - m
		sum += d * d
	}
	return sqrt(sum / float64(len(values)-1))
}

func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	x := v
	for i := 0; i < 40; i++ {
		x = 0.5 * (x + v/x)
	}
	return x
}

func pearson(xs, ys []float64) float64 {
	n := len(xs)
	if n < 3 || n != len(ys) {
		return 0
	}
	mx := mean(xs)
	my := mean(ys)
	var num, dx, dy float64
	for i := 0; i < n; i++ {
		a := xs[i] - mx
		b := ys[i] - my
		num += a * b
		dx += a * a
		dy += b * b
	}
	if dx == 0 || dy == 0 {
		return 0
	}
	return num / sqrt(dx*dy)
}
