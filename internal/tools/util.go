package tools

import (
	"hash/fnv"
	"math"
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
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	}
	if strings.Contains(s, ",") {
		if strings.Contains(s, ".") {
			// Both separators: the last one is the decimal separator.
			if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
				s = strings.ReplaceAll(s, ".", "")
				s = strings.ReplaceAll(s, ",", ".")
			} else {
				s = strings.ReplaceAll(s, ",", "")
			}
		} else if commaIsThousands(s) {
			s = strings.ReplaceAll(s, ",", "")
		} else {
			s = strings.ReplaceAll(s, ",", ".")
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// commaIsThousands reports whether every comma-separated group after the first
// has exactly three digits ("1,234", "1,234,567"), which is a thousands
// separator rather than a decimal mark ("1,5").
func commaIsThousands(s string) bool {
	parts := strings.Split(s, ",")
	if len(parts) < 2 || len(parts[0]) == 0 || len(parts[0]) > 3 {
		return false
	}
	for _, part := range parts[1:] {
		if len(part) != 3 {
			return false
		}
	}
	return true
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
		case v >= 1e17: // nanoseconds
			return time.Unix(0, v), true
		case v >= 1e14: // microseconds
			return time.UnixMicro(v), true
		case v >= 1e11: // milliseconds (1973+)
			return time.UnixMilli(v), true
		case v >= 1e8: // seconds (1973+)
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
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "ts" || strings.HasPrefix(n, "ts_") || strings.HasSuffix(n, "_ts") {
		return true
	}
	// Whole-token matching avoids false positives such as "update", "runtime"
	// or "lifetime" that merely contain a keyword.
	tokens := strings.FieldsFunc(n, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'а' && r <= 'я' || r >= '0' && r <= '9')
	})
	for _, token := range tokens {
		switch token {
		case "time", "date", "timestamp", "datetime", "дата", "время", "период":
			return true
		}
	}
	return false
}

func xmlEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\'':
			b.WriteString("&apos;")
		case r == 0x9 || r == 0xA || r == 0xD ||
			(r >= 0x20 && r <= 0xD7FF) ||
			(r >= 0xE000 && r <= 0xFFFD) ||
			(r >= 0x10000 && r <= 0x10FFFF):
			b.WriteRune(r)
		default:
			// Control characters and surrogates are illegal in XML 1.0.
		}
	}
	return b.String()
}

func shortHash(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

// truncateRunes cuts s to at most n bytes without splitting a UTF-8 rune.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
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

// sqrt delegates to math.Sqrt: the previous hand-rolled Newton iteration
// started at x=v and halved toward the root, so large magnitudes (for example
// 1e14) returned wrong values after a fixed iteration count.
func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
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
