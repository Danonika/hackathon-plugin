package config

import (
	"os"
	"path/filepath"
	"strings"
)

func LoadDotEnv(root string) {
	data, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
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
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

func unquoteEnvValue(value string) string {
	if len(value) > 0 {
		if q := value[0]; q == '"' || q == '\'' {
			if end := strings.IndexByte(value[1:], q); end >= 0 {
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
