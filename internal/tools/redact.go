package tools

import "strings"

// redactBackendError strips credentials from a driver/connection error before
// it is surfaced to an MCP caller (DSNs can appear in Postgres and MinIO
// errors). It mirrors the cmd/hackathonkit-mcp helper so tool warnings do not leak
// secrets or backend details across the tool boundary.
func redactBackendError(err error) string {
	if err == nil {
		return ""
	}
	text := redactURLUserinfo(err.Error())
	// Longest keys first so "secret key" is not consumed by "secret".
	for _, key := range []string{
		"secret_key", "secret key", "access_key", "access key",
		"api_key", "api key", "apikey",
		"password", "passwd", "secret", "token", "credential", "bearer",
	} {
		// Cover key=value, key: value and key value renderings.
		text = redactMarkerValues(text, key+"=")
		text = redactMarkerValues(text, key+":")
		text = redactMarkerValues(text, key+" ")
	}
	return text
}

// redactURLUserinfo replaces the userinfo of any URL (scheme://user:pass@host)
// with [redacted], leaving the rest of the authority intact.
func redactURLUserinfo(text string) string {
	var b strings.Builder
	i := 0
	for {
		idx := strings.Index(text[i:], "://")
		if idx < 0 {
			b.WriteString(text[i:])
			break
		}
		idx += i
		b.WriteString(text[i : idx+3])
		rest := text[idx+3:]
		// Bound the authority by whitespace/quotes only: a password may contain
		// '/', '?' or '#' unencoded, and cutting at those would place the '@'
		// after the cut and leak the credential. Over-redacting a benign
		// `host/path@x` is preferred to a leak.
		end := len(rest)
		for j := 0; j < len(rest); j++ {
			switch rest[j] {
			case ' ', '\t', '\n', '\r', '"', '\'':
				end = j
			}
			if end == j {
				break
			}
		}
		authority := rest[:end]
		if at := strings.LastIndex(authority, "@"); at > 0 {
			b.WriteString("[redacted]")
			b.WriteString(authority[at:])
		} else {
			b.WriteString(authority)
		}
		i = idx + 3 + end
	}
	return b.String()
}

// redactMarkerValues replaces the value that follows every occurrence of
// marker, handling quoted values (e.g. password="a b") so the whole secret is
// removed rather than only up to the first space.
func redactMarkerValues(text, marker string) string {
	const replacement = "[redacted]"
	searchFrom := 0
	for {
		idx := indexFold(text[searchFrom:], marker)
		if idx < 0 {
			break
		}
		idx += searchFrom
		valueStart := idx + len(marker)
		// The separator marker consumes one delimiter; skip any remaining
		// whitespace and a stray '='/':' so "key = value" / "key : value"
		// (spaces around the separator) is redacted too.
		skipSeparator := func(p int) int {
			for p < len(text) && (text[p] == ' ' || text[p] == '\t') {
				p++
			}
			if p < len(text) && (text[p] == '=' || text[p] == ':') {
				p++
				for p < len(text) && (text[p] == ' ' || text[p] == '\t') {
					p++
				}
			}
			return p
		}
		valueStart = skipSeparator(valueStart)
		quote := byte(0)
		if valueStart < len(text) && (text[valueStart] == '"' || text[valueStart] == '\'') {
			quote = text[valueStart]
			valueStart++
		}
		end := valueStart
		if quote != 0 {
			for end < len(text) && text[end] != quote {
				end++
			}
		} else {
			for end < len(text) && !isValueDelimiter(text[end]) {
				end++
			}
		}
		if end == valueStart {
			// Empty value: nothing to redact; advance to avoid a loop.
			searchFrom = idx + len(marker)
			continue
		}
		text = text[:valueStart] + replacement + text[end:]
		searchFrom = valueStart + len(replacement)
	}
	return text
}

func isValueDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '&', '"', '\'':
		return true
	}
	return false
}

// indexFold is a byte-length-preserving case-insensitive substring search, so
// offsets computed on the haystack remain valid for the original string.
func indexFold(haystack, needle string) int {
	if needle == "" {
		return 0
	}
	limit := len(haystack) - len(needle)
	for i := 0; i <= limit; i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if lowerASCII(haystack[i+j]) != lowerASCII(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
