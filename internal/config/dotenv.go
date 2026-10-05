package config

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

func LoadDotEnv(root string) {
	data, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("dotenv: cannot read %s: %v", filepath.Join(root, ".env"), err)
		}
		return
	}
	fileSet := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		line = stripExportPrefix(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			continue
		}
		value := strings.TrimSpace(line[eq+1:])
		value = unquoteEnvValue(value)
		if !validEnvKey(key) {
			log.Printf("dotenv: ignoring invalid key %q", key)
			continue
		}
		// A key already present in the process environment wins, but a later
		// duplicate line in this file overrides an earlier one.
		if _, set := os.LookupEnv(key); !set || fileSet[key] {
			if err := os.Setenv(key, value); err != nil {
				log.Printf("dotenv: cannot set %s: %v", key, err)
			} else {
				fileSet[key] = true
			}
		}
	}
}

// stripExportPrefix removes a leading `export` keyword followed by whitespace,
// accepting any whitespace and letter case, while leaving a real key such as
// `exportFOO=1` untouched.
func stripExportPrefix(line string) string {
	if len(line) < len("export") || !strings.EqualFold(line[:len("export")], "export") {
		return line
	}
	rest := line[len("export"):]
	if rest == "" || !(rest[0] == ' ' || rest[0] == '\t') {
		return line
	}
	return strings.TrimSpace(rest)
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func unquoteEnvValue(value string) string {
	if len(value) > 0 {
		switch value[0] {
		case '"':
			runes := []rune(value)
			var b strings.Builder
			escaped := false
			for i := 1; i < len(runes); i++ {
				r := runes[i]
				if escaped {
					switch r {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					case '\\', '"', '$', '`':
						b.WriteRune(r)
					default:
						// Preserve the backslash for unrecognized escapes.
						b.WriteByte('\\')
						b.WriteRune(r)
					}
					escaped = false
					continue
				}
				if r == '\\' {
					escaped = true
					continue
				}
				if r == '"' {
					if isClosingQuote(runes, i) {
						return b.String()
					}
					// An interior quote (e.g. KEY="abc"def) is not a
					// terminator: keep it rather than silently dropping the
					// rest of the value.
					b.WriteRune(r)
					continue
				}
				b.WriteRune(r)
			}
			return b.String()
		case '\'':
			runes := []rune(value)
			for i := 1; i < len(runes); i++ {
				if runes[i] == '\'' && isClosingQuote(runes, i) {
					return string(runes[1:i])
				}
			}
			return value[1:]
		}
	}
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}

// isClosingQuote reports whether the quote at index idx terminates the value:
// only when the remainder is empty, whitespace, or a `#` comment. Any other
// remainder means the quote is interior (e.g. KEY="abc"def) and must not
// truncate the value.
func isClosingQuote(runes []rune, idx int) bool {
	rest := strings.TrimSpace(string(runes[idx+1:]))
	return rest == "" || strings.HasPrefix(rest, "#")
}
