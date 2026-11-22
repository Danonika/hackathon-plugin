package tools

import "testing"

func TestRedactBackendErrorURLAndSpacedSeparator(t *testing.T) {
	cases := map[string]string{
		"postgres://user:sample password@host/db": "postgres://[redacted]@host/db",
		"postgres://user:pa/ss@host/db":           "postgres://[redacted]@host/db",
		"postgres://user:pa?ss@host/db":           "postgres://[redacted]@host/db",
		"connect failed: password = s3cr3t":       "connect failed: password = [redacted]",
	}
	for in, want := range cases {
		if got := RedactBackendError(errStr(in)); got != want {
			t.Errorf("redact(%q) = %q, want %q", in, got, want)
		}
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }
