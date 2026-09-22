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
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
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
		if _, set := os.LookupEnv(key); !set {
			if err := os.Setenv(key, value); err != nil {
				log.Printf("dotenv: cannot set %s: %v", key, err)
			}
		}
	}
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
		switch q := value[0]; q {
		case '"':
			var b strings.Builder
			escaped := false
			for _, r := range value[1:] {
				if escaped {
					switch r {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					default:
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
					return b.String()
				}
				b.WriteRune(r)
			}
			return b.String()
		case '\'':
			if end := strings.IndexByte(value[1:], '\''); end >= 0 {
				return value[1 : 1+end]
			}
			return value[1:]
		}
	}
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}
